package api

import "net/http"

type segmentUpdateRequest struct {
	Name      string  `json:"name"`
	Tension   float64 `json:"tension"`
	Available bool    `json:"available"`
}

func (handler *Handler) webPath(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, handler.runtime.WebPath())
}

func (handler *Handler) updateWebSegment(writer http.ResponseWriter, request *http.Request) {
	var input segmentUpdateRequest
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if err := handler.runtime.UpdateWebSegment(input.Name, input.Tension, input.Available); err != nil {
		writeError(writer, http.StatusConflict, err)
		return
	}
	writeJSON(writer, http.StatusAccepted, handler.runtime.WebPath())
}

func (handler *Handler) webRecoveries(writer http.ResponseWriter, request *http.Request) {
	writeJSON(writer, http.StatusOK, handler.runtime.WebRecoveries())
}
