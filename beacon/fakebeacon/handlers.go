package fakebeacon

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/prysmaticlabs/prysm/v5/api/server/structs"
	"github.com/prysmaticlabs/prysm/v5/network/httputil"
)

const MaxBlobsPerBlock = 6

var (
	versionMethod        = "/eth/v1/node/version"
	specMethod           = "/eth/v1/config/spec"
	genesisMethod        = "/eth/v1/beacon/genesis"
	sidecarsMethodPrefix = "/eth/v1/beacon/blob_sidecars/{slot}"
)

func VersionMethod(w http.ResponseWriter, r *http.Request) {
	resp := &structs.GetVersionResponse{
		Data: &structs.Version{
			Version: "",
		},
	}
	httputil.WriteJson(w, resp)
}

func SpecMethod(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJson(w, &structs.GetSpecResponse{Data: configSpec()})
}

func GenesisMethod(w http.ResponseWriter, r *http.Request) {
	httputil.WriteJson(w, beaconGenesis())
}

func (s *Service) SidecarsMethod(w http.ResponseWriter, r *http.Request) {
	indices, err := parseIndices(r.URL)
	if err != nil {
		httputil.HandleError(w, err.Error(), http.StatusBadRequest)
		return
	}
	segments := strings.Split(r.URL.Path, "/")
	slot, err := strconv.ParseUint(segments[len(segments)-1], 10, 64)
	if err != nil {
		httputil.HandleError(w, "not a valid slot(timestamp)", http.StatusBadRequest)
		return
	}

	resp, err := beaconBlobSidecars(r.Context(), s.backend, slot, indices)
	if err != nil {
		httputil.HandleError(w, err.Error(), http.StatusBadRequest)
		return
	}
	httputil.WriteJson(w, resp)
}

// parseIndices filters out invalid and duplicate blob indices
func parseIndices(url *url.URL) ([]int, error) {
	rawIndices := url.Query()["indices"]
	indices := make([]int, 0, MaxBlobsPerBlock)
	invalidIndices := make([]string, 0)
loop:
	for _, raw := range rawIndices {
		ix, err := strconv.Atoi(raw)
		if err != nil {
			invalidIndices = append(invalidIndices, raw)
			continue
		}
		if ix >= MaxBlobsPerBlock {
			invalidIndices = append(invalidIndices, raw)
			continue
		}
		for i := range indices {
			if ix == indices[i] {
				continue loop
			}
		}
		indices = append(indices, ix)
	}

	if len(invalidIndices) > 0 {
		return nil, fmt.Errorf("requested blob indices %v are invalid", invalidIndices)
	}
	return indices, nil
}
