package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/Jille/convreq"
	"github.com/Jille/convreq/respond"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	ssi "github.com/nuts-foundation/go-did"
	"github.com/nuts-foundation/go-did/did"

	"github.com/bluesky-social/indigo/atproto/crypto"

	"bsky.watch/plc-mirror/util/plc"
)

type Server struct {
	db     *gorm.DB
	mirror *Mirror

	MaxDelay time.Duration

	handler http.HandlerFunc
}

func NewServer(ctx context.Context, db *gorm.DB, mirror *Mirror) (*Server, error) {
	s := &Server{
		db:       db,
		mirror:   mirror,
		MaxDelay: 5 * time.Minute,
	}
	s.handler = convreq.Wrap(s.serve)
	return s, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	s.handler(w, req)
}

func (s *Server) Ready(w http.ResponseWriter, req *http.Request) {
	convreq.Wrap(func(ctx context.Context) convreq.HttpResponse {
		ts, err := s.mirror.LastRecordTimestamp(ctx)
		if err != nil {
			return respond.InternalServerError(err.Error())
		}
		delay := time.Since(ts)
		if delay > s.MaxDelay {
			return respond.ServiceUnavailable(fmt.Sprintf("still %s behind", delay))
		}
		return respond.String("OK")
	})(w, req)
}

func (s *Server) serve(ctx context.Context, req *http.Request) convreq.HttpResponse {
	start := time.Now()
	updateMetrics := func(c int) {
		requestCount.WithLabelValues(fmt.Sprint(c)).Inc()
		requestLatency.WithLabelValues(fmt.Sprint(c)).Observe(float64(time.Now().Sub(start)) / float64(time.Millisecond))
	}

	// Check if the mirror is up to date.
	ts, err := s.mirror.LastRecordTimestamp(ctx)
	if err != nil {
		return respond.InternalServerError(err.Error())
	}
	delay := time.Since(ts)
	if delay > s.MaxDelay {
		// Check LastCompletion and if it's recent enough - that means
		// that we're actually caught up and there simply aren't any recent
		// PLC operations.
		completionDelay := time.Since(s.mirror.LastCompletion())
		if completionDelay > s.MaxDelay {
			updateMetrics(http.StatusServiceUnavailable)
			return respond.ServiceUnavailable(fmt.Sprintf("mirror is %s behind", delay))
		}
	}
	log := zerolog.Ctx(ctx)

	requestedDid := strings.ToLower(strings.TrimPrefix(req.URL.Path, "/"))
	if len(requestedDid) == 0 {
		return respond.NotFound("missing DID parameter")
	}

	var entry PLCLogEntry
	err = s.db.Model(&entry).Where("did = ? AND (NOT nullified)", requestedDid).Order("plc_timestamp desc").Limit(1).Take(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		updateMetrics(http.StatusNotFound)
		return respond.NotFound("unknown DID")
	}
	if err != nil {
		log.Error().Err(err).Str("did", requestedDid).Msgf("Failed to get the last log entry for %q: %s", requestedDid, err)
		updateMetrics(http.StatusInternalServerError)
		return respond.InternalServerError("failed to get the last log entry")
	}

	if _, ok := entry.Operation.Value.(plc.Tombstone); ok {
		updateMetrics(http.StatusNotFound)
		return respond.NotFound("DID deleted")
	}

	var op plc.Op
	switch v := entry.Operation.Value.(type) {
	case plc.Op:
		op = v
	case plc.LegacyCreateOp:
		op = v.AsUnsignedOp()
	}

	didValue := did.DID{
		Method: "plc",
		ID:     strings.TrimPrefix(entry.DID, "did:plc:"),
	}
	r := did.Document{
		Context: []interface{}{
			"https://www.w3.org/ns/did/v1",
			"https://w3id.org/security/multikey/v1"},
		ID:          didValue,
		AlsoKnownAs: mapSlice(op.AlsoKnownAs, ssi.MustParseURI),
	}

	for id, s := range op.Services {
		r.Service = append(r.Service, did.Service{
			ID:              ssi.MustParseURI("#" + id),
			Type:            s.Type,
			ServiceEndpoint: s.Endpoint,
		})
	}

	for id, m := range op.VerificationMethods {
		idValue := did.DIDURL{
			DID:      didValue,
			Fragment: id,
		}
		r.VerificationMethod.Add(&did.VerificationMethod{
			ID:                 idValue,
			Type:               "Multikey",
			Controller:         didValue,
			PublicKeyMultibase: strings.TrimPrefix(m, "did:key:"),
		})

		key, err := crypto.ParsePublicDIDKey(m)
		if err == nil {
			context := ""
			switch key.(type) {
			case *crypto.PublicKeyK256:
				context = "https://w3id.org/security/suites/secp256k1-2019/v1"
			case *crypto.PublicKeyP256:
				context = "https://w3id.org/security/suites/ecdsa-2019/v1"
			}
			if context != "" && !slices.Contains(r.Context, interface{}(context)) {
				r.Context = append(r.Context, context)
			}
		}
	}
	updateMetrics(http.StatusOK)
	return respond.JSON(r)
}

func mapSlice[A any, B any](s []A, fn func(A) B) []B {
	r := make([]B, 0, len(s))
	for _, v := range s {
		r = append(r, fn(v))
	}
	return r
}
