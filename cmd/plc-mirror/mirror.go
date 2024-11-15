package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"bsky.watch/plc-mirror/models"
	"bsky.watch/plc-mirror/util/plc"
)

type PLCLogEntry struct {
	ID        models.ID `gorm:"primarykey"`
	CreatedAt time.Time

	DID          string        `gorm:"column:did;index:did_timestamp;uniqueIndex:did_cid"`
	CID          string        `gorm:"column:cid;uniqueIndex:did_cid"`
	PLCTimestamp string        `gorm:"column:plc_timestamp;index:did_timestamp,sort:desc;index:,sort:desc"`
	Nullified    bool          `gorm:"default:false"`
	Operation    plc.Operation `gorm:"type:JSONB;serializer:json"`
}

type Mirror struct {
	db       *gorm.DB
	upstream *url.URL
	limiter  *rate.Limiter

	mu                      sync.RWMutex
	lastCompletionTimestamp time.Time
	lastRecordTimestamp     time.Time
}

func NewMirror(ctx context.Context, upstream string, db *gorm.DB) (*Mirror, error) {
	u, err := url.Parse(upstream)
	if err != nil {
		return nil, err
	}
	u.Path, err = url.JoinPath(u.Path, "export")
	if err != nil {
		return nil, err
	}
	return &Mirror{
		db:       db,
		upstream: u,
		// Current rate limit is `500 per five minutes`, lets stay a bit under it.
		limiter: rate.NewLimiter(rate.Limit(450.0/300), 4),
	}, nil
}

func (m *Mirror) Start(ctx context.Context) error {
	go m.run(ctx)
	return nil
}

func (m *Mirror) run(ctx context.Context) {
	log := zerolog.Ctx(ctx).With().Str("module", "mirror").Logger()
	for {
		select {
		case <-ctx.Done():
			log.Info().Msgf("PLC mirror stopped")
			return
		default:
			if err := m.runOnce(ctx); err != nil {
				if ctx.Err() == nil {
					log.Error().Err(err).Msgf("Failed to get new log entries from PLC: %s", err)
				}
			} else {
				now := time.Now()
				m.mu.Lock()
				m.lastCompletionTimestamp = now
				m.mu.Unlock()
			}
			time.Sleep(10 * time.Second)
		}
	}
}

func (m *Mirror) LastCompletion() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCompletionTimestamp
}

func (m *Mirror) LastRecordTimestamp(ctx context.Context) (time.Time, error) {
	m.mu.RLock()
	t := m.lastRecordTimestamp
	m.mu.RUnlock()
	if !t.IsZero() {
		return t, nil
	}

	ts := ""
	err := m.db.WithContext(ctx).Model(&PLCLogEntry{}).Select("plc_timestamp").Order("plc_timestamp desc").Limit(1).Take(&ts).Error
	if err != nil {
		return time.Time{}, err
	}
	dbTimestamp, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing timestamp %q: %w", ts, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lastRecordTimestamp.IsZero() {
		m.lastRecordTimestamp = dbTimestamp
	}
	if m.lastRecordTimestamp.After(dbTimestamp) {
		return m.lastRecordTimestamp, nil
	}
	return dbTimestamp, nil
}

func (m *Mirror) runOnce(ctx context.Context) error {
	log := zerolog.Ctx(ctx)

	cursor := ""
	err := m.db.Model(&PLCLogEntry{}).Select("plc_timestamp").Order("plc_timestamp desc").Limit(1).Take(&cursor).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("failed to get the cursor: %w", err)
	}

	u := *m.upstream

	for {
		params := u.Query()
		params.Set("count", "1000")
		if cursor != "" {
			params.Set("after", cursor)
		}
		u.RawQuery = params.Encode()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return fmt.Errorf("constructing request: %w", err)
		}

		_ = m.limiter.Wait(ctx)
		log.Info().Msgf("Listing PLC log entries with cursor %q...", cursor)
		log.Debug().Msgf("Request URL: %s", u.String())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("sending request: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}

		newEntries := []PLCLogEntry{}
		decoder := json.NewDecoder(resp.Body)
		oldCursor := cursor

		var lastTimestamp time.Time

		for {
			var entry plc.OperationLogEntry
			err := decoder.Decode(&entry)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return fmt.Errorf("parsing log entry: %w", err)
			}

			cursor = entry.CreatedAt
			row := *FromOperationLogEntry(entry)
			newEntries = append(newEntries, row)

			t, err := time.Parse(time.RFC3339, row.PLCTimestamp)
			if err == nil {
				lastEventTimestamp.Set(float64(t.Unix()))
				lastTimestamp = t
			} else {
				log.Warn().Msgf("Failed to parse %q: %s", row.PLCTimestamp, err)
			}
		}

		if len(newEntries) == 0 || cursor == oldCursor {
			log.Info().Msg("Got no new messages, throttling...")
			break
		}

		err = m.db.Clauses(
			clause.OnConflict{
				Columns:   []clause.Column{{Name: "did"}, {Name: "cid"}},
				DoNothing: true,
			},
		).Create(newEntries).Error
		if err != nil {
			return fmt.Errorf("inserting log entry into database: %w", err)
		}

		if !lastTimestamp.IsZero() {
			m.mu.Lock()
			m.lastRecordTimestamp = lastTimestamp
			m.mu.Unlock()
		}

		log.Info().Msgf("Got %d log entries. New cursor: %q", len(newEntries), cursor)

		if len(newEntries) < 1000 {
			log.Info().Msg("Got fewer than 1000 messages, throttling...")
			break
		}
	}
	return nil
}

func FromOperationLogEntry(op plc.OperationLogEntry) *PLCLogEntry {
	return &PLCLogEntry{
		DID:          op.DID,
		CID:          op.CID,
		PLCTimestamp: op.CreatedAt,
		Nullified:    op.Nullified,
		Operation:    op.Operation,
	}
}
