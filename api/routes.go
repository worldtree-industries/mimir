package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/go-chi/chi/v5"

	"github.com/worldtree-industries/mimir/models"
	"github.com/worldtree-industries/mimir/service"
)

// Routes registers mimir-server's routes on r:
//
//	POST /v1/registrations  Registration, from clients (204)
//	POST /v1/overrides      create an Override (201, the Override with its ID)
//
// Errors are RFC 9457 problems. There is no authentication: the admin API is
// for the private network only.
func Routes(r chi.Router, svc *service.Service, logger *slog.Logger) {
	h := handlers{svc: svc, codec: httpx.NewCodec(httpx.DefaultMaxBodyBytes)}
	handle := func(fn func(http.ResponseWriter, *http.Request) error) http.HandlerFunc {
		return httpx.HandlerWithLogger(fn, logger)
	}

	r.Post("/v1/registrations", handle(h.register))
	r.Post("/v1/overrides", handle(h.createOverride))
}

type handlers struct {
	svc   *service.Service
	codec httpx.Codec
}

func (h handlers) register(w http.ResponseWriter, r *http.Request) error {
	var reg models.Registration
	if err := h.codec.Decode(w, r, &reg); err != nil {
		return err
	}
	if err := h.svc.Register(r.Context(), reg); err != nil {
		return problem(err)
	}
	w.WriteHeader(http.StatusNoContent)

	return nil
}

func (h handlers) createOverride(w http.ResponseWriter, r *http.Request) error {
	var o models.Override
	if err := h.codec.Decode(w, r, &o); err != nil {
		return err
	}
	created, err := h.svc.CreateOverride(r.Context(), o)
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusCreated, created)
}

// problem turns a service error into the problem the client is sent.
func problem(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalid):
		return httpx.UnprocessableEntity("invalid_request", err.Error())
	case errors.Is(err, service.ErrUnknownItem):
		return httpx.UnprocessableEntity("unknown_item", err.Error())
	case errors.Is(err, service.ErrOverrideExists):
		return httpx.Conflict("override_exists", err.Error())
	default:
		return httpx.Internal(err)
	}
}
