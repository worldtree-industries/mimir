package api

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strconv"

	"github.com/dfryer1193/mjolnir/utils/httpx"
	"github.com/go-chi/chi/v5"

	"github.com/worldtree-industries/mimir/models"
	"github.com/worldtree-industries/mimir/service"
)

// Routes registers mimir-server's routes on r:
//
//	POST /v1/registrations  Registration, from clients (204)
//	POST /v1/overrides      create an Override (201, the Override with its ID)
//	PUT /v1/overrides/{id}  set an Override's value (body {"value": ...}; 200, the Override)
//	DELETE /v1/overrides/{id}  delete an Override (204)
//	GET /v1/overrides       list a Config's Overrides: ?config= (required), and
//	                        optionally ?deployable= and ?environment=, each an
//	                        exact match (empty means not narrowed) or, if
//	                        absent, any (200, {"overrides": [...]})
//	GET /v1/explain         ?config=&deployable=&environment= the Namespace's
//	                        Items, each with its effective value and Source
//	                        (200, models.Explanation)
//	GET  /v1/history        the History of Override changes, oldest first (200);
//	                        filter with ?config=, ?deployable= and
//	                        ?environment=, which match as for GET /v1/overrides
//	                        except that config is optional
//	GET  /v1/configs        the Configs Mimir knows (200)
//	GET  /v1/items          the Items Mimir knows, with when they were last
//	                        registered (200); narrow with the config query
//	                        parameter
//	GET  /v1/namespaces     the Namespaces Mimir knows (200); narrow with the
//	                        config query parameter
//
// Writes are attributed to the X-Mimir-Caller header, else to the address the
// request came from.
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
	r.Get("/v1/overrides", handle(h.listOverrides))
	r.Get("/v1/explain", handle(h.explain))
	r.Put("/v1/overrides/{id}", handle(h.updateOverride))
	r.Delete("/v1/overrides/{id}", handle(h.deleteOverride))
	r.Get("/v1/history", handle(h.history))
	r.Get("/v1/configs", handle(h.configs))
	r.Get("/v1/items", handle(h.items))
	r.Get("/v1/namespaces", handle(h.namespaces))
	r.Get("/v1/environments", handle(h.environments))
}

func (h handlers) configs(w http.ResponseWriter, r *http.Request) error {
	configs, err := h.svc.Configs(r.Context())
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, configs)
}

func (h handlers) items(w http.ResponseWriter, r *http.Request) error {
	items, err := h.svc.Items(r.Context(), r.URL.Query().Get("config"))
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, items)
}

func (h handlers) namespaces(w http.ResponseWriter, r *http.Request) error {
	namespaces, err := h.svc.Namespaces(r.Context(), r.URL.Query().Get("config"))
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, namespaces)
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

func (h handlers) environments(w http.ResponseWriter, r *http.Request) error {
	names, err := h.svc.Environments(r.Context())
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, map[string][]string{"environments": names})
}

func (h handlers) createOverride(w http.ResponseWriter, r *http.Request) error {
	var o models.Override
	if err := h.codec.Decode(w, r, &o); err != nil {
		return err
	}
	created, err := h.svc.CreateOverride(callerContext(r), o)
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusCreated, created)
}

// scopeFilter reads the config, deployable and environment query parameters
// as a ScopeFilter: an absent deployable or environment matches any, and a
// present one matches exactly, empty meaning not narrowed.
func scopeFilter(r *http.Request) models.ScopeFilter {
	q := r.URL.Query()
	filter := models.ScopeFilter{Config: q.Get("config")}
	if q.Has("deployable") {
		deployable := q.Get("deployable")
		filter.Deployable = &deployable
	}
	if q.Has("environment") {
		environment := q.Get("environment")
		filter.Environment = &environment
	}

	return filter
}

func (h handlers) listOverrides(w http.ResponseWriter, r *http.Request) error {
	overrides, err := h.svc.ListOverrides(r.Context(), scopeFilter(r))
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, map[string][]models.Override{"overrides": overrides})
}

func (h handlers) explain(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	explanation, err := h.svc.Explain(r.Context(), models.Namespace{
		Config:      q.Get("config"),
		Deployable:  q.Get("deployable"),
		Environment: q.Get("environment"),
	})
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, explanation)
}

func (h handlers) updateOverride(w http.ResponseWriter, r *http.Request) error {
	id, err := overrideID(r)
	if err != nil {
		return err
	}
	var body struct {
		Value string `json:"value"`
	}
	if err = h.codec.Decode(w, r, &body); err != nil {
		return err
	}
	updated, err := h.svc.UpdateOverride(callerContext(r), id, body.Value)
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, updated)
}

func (h handlers) deleteOverride(w http.ResponseWriter, r *http.Request) error {
	id, err := overrideID(r)
	if err != nil {
		return err
	}
	if err = h.svc.DeleteOverride(callerContext(r), id); err != nil {
		return problem(err)
	}
	w.WriteHeader(http.StatusNoContent)

	return nil
}

// overrideID reads the {id} of an Override's path.
func overrideID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id < 1 {
		return 0, httpx.BadRequest("invalid_id", "the Override ID must be a positive integer")
	}

	return id, nil
}

// problem turns a service error into the problem the client is sent.
func problem(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalid):
		return httpx.UnprocessableEntity("invalid_request", err.Error())
	case errors.Is(err, service.ErrUnknownItem):
		return httpx.UnprocessableEntity("unknown_item", err.Error())
	case errors.Is(err, service.ErrUnknownNamespace):
		return httpx.NotFound("unknown_namespace", err.Error())
	case errors.Is(err, service.ErrOverrideNotFound):
		return httpx.NotFound("override_not_found", err.Error())
	case errors.Is(err, service.ErrOverrideExists):
		return httpx.Conflict("override_exists", err.Error())
	default:
		return httpx.Internal(err)
	}
}

func (h handlers) history(w http.ResponseWriter, r *http.Request) error {
	entries, err := h.svc.History(r.Context(), scopeFilter(r))
	if err != nil {
		return problem(err)
	}

	return h.codec.Respond(w, r, http.StatusOK, entries)
}

// callerHeader is the header with which a caller says who they are.
const callerHeader = "X-Mimir-Caller"

// callerContext is r's context, carrying who made the request as best known:
// the name in the caller header, else the address it came from. The admin API
// has no authentication, so neither is verified.
func callerContext(r *http.Request) context.Context {
	caller := r.Header.Get(callerHeader)
	if caller == "" {
		caller = r.RemoteAddr
		if host, _, err := net.SplitHostPort(caller); err == nil {
			caller = host
		}
	}

	return service.WithCaller(r.Context(), caller)
}
