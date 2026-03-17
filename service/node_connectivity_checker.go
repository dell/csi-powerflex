package service

/*
 *
 * Copyright © 2025 Dell Inc. or its subsidiaries. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	csictx "github.com/dell/gocsi/context"
	"github.com/gorilla/mux"
)

// pollingFrequency in seconds
var pollingFrequencyInSeconds int64

// startAPIService reads nodes to array status periodically
func (s *service) startAPIService(ctx context.Context) {
	if !s.opts.IsPodmonEnabled {
		log.Info("podmon is not enabled")
		return
	}
	atomic.StoreInt64(&pollingFrequencyInSeconds, SetPollingFrequency(ctx))
	s.startNodeToArrayConnectivityCheck(ctx)
	s.apiRouter(ctx)
}

// apiRouter serves http requests
func (s *service) apiRouter(_ context.Context) {
	log.Infof("starting http server on port %s", s.opts.PodmonPort)
	// create a new mux router
	router := mux.NewRouter()
	// route to connectivity status
	// connectivityStatus is the handlers
	router.HandleFunc(ArrayStatus, s.connectivityStatus).Methods("GET")
	router.HandleFunc(ArrayStatus+"/"+"{systemID}", s.getArrayConnectivityStatus).Methods("GET")
	// start http server to serve requests
	server := &http.Server{
		Addr:         s.opts.PodmonPort,
		Handler:      router,
		ReadTimeout:  Timeout,
		WriteTimeout: Timeout,
	}
	err := server.ListenAndServe()
	if err != nil {
		log.Errorf("unable to start http server to serve status requests due to %s", err)
	}
	log.Infof("started http server to serve status requests at %s", s.opts.PodmonPort)
}

// connectivityStatus handler returns array connectivity status
func (s *service) connectivityStatus(w http.ResponseWriter, _ *http.Request) {
	log.Infof("connectivityStatus called, status is %v \n", s.probeStatus)
	// w.Header().Set("Content-Type", "application/json")
	if s.probeStatus == nil {
		log.Errorf("error probeStatus map in cache is empty")
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		return
	}

	// convert struct to JSON
	log.Debugf("ProbeStatus fetched from the cache has %+v", s.probeStatus)

	jsonResponse, err := MarshalSyncMapToJSON(s.probeStatus)
	if err != nil {
		log.Errorf("error %s during marshaling to json", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		return
	}
	log.Info("sending connectivityStatus for all arrays ")
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(jsonResponse)
	if err != nil {
		log.Errorf("unable to write response %s", err)
	}
}

// MarshalSyncMapToJSON marshal the sync Map to Json
func MarshalSyncMapToJSON(m *sync.Map) ([]byte, error) {
	tmpMap := make(map[string]ArrayConnectivityStatus)
	m.Range(func(k, value interface{}) bool {
		// this check is not necessary but just in case is someone in future play around this
		switch value.(type) {
		case ArrayConnectivityStatus:
			tmpMap[k.(string)] = value.(ArrayConnectivityStatus)
			return true
		default:
			log.Errorf("invalid data is stored in cache")
			return false
		}
	})
	log.Debugf("map value is %+v", tmpMap)
	if len(tmpMap) == 0 {
		return nil, fmt.Errorf("invalid data is stored in cache")
	}
	return json.Marshal(tmpMap)
}

// getArrayConnectivityStatus handler lists status of the requested array
func (s *service) getArrayConnectivityStatus(w http.ResponseWriter, r *http.Request) {
	systemID := mux.Vars(r)["systemID"]
	log.Infof("GetArrayConnectivityStatus called for array %s \n", systemID)
	status, found := s.probeStatus.Load(systemID)
	if !found {
		// specify status code
		w.WriteHeader(http.StatusNotFound)
		w.Header().Set("Content-Type", "application/json")
		// update response writer
		fmt.Fprintf(w, "array %s not found \n", systemID)
		return
	}
	// convert status struct to JSON
	jsonResponse, err := json.Marshal(status)
	if err != nil {
		log.Errorf("error %s during marshaling to json", err)
		w.WriteHeader(http.StatusInternalServerError)
		w.Header().Set("Content-Type", "application/json")
		return
	}
	log.Infof("sending response %+v for array %s \n", status, systemID)
	// update response
	_, err = w.Write(jsonResponse)
	if err != nil {
		log.Errorf("unable to write response %s", err)
	}
}

// startNodeToArrayConnectivityCheck starts connectivityTest as one goroutine for each array
func (s *service) startNodeToArrayConnectivityCheck(ctx context.Context) {
	log.Debug("startNodeToArrayConnectivityCheck called")
	s.probeStatus = new(sync.Map)

	for _, array := range s.opts.arrays {
		go s.testConnectivityAndUpdateStatus(ctx, array.SystemID, Timeout)
	}

	log.Infof("startNodeToArrayConnectivityCheck is running probes at pollingFrequency %d ", pollingFrequencyInSeconds/2)
}

// testConnectivityAndUpdateStatus runs probe to test connectivity from node to array
// updates probeStatus map[array]ArrayConnectivityStatus
func (s *service) testConnectivityAndUpdateStatus(ctx context.Context, systemID string, timeout time.Duration) {
	defer func() {
		if err := recover(); err != nil {
			log.Errorf("panic occurred in testConnectivityAndUpdateStatus: %s", err)
		}
		// if panic occurs restart new goroutine
		go s.testConnectivityAndUpdateStatus(ctx, systemID, timeout)
	}()
	var status ArrayConnectivityStatus
	for {
		select {
		case <-ctx.Done():
			log.Debugf("Context cancelled, stopping connectivity probe for array %s", systemID)
			return
		default:
		}
		// add timeout to context
		timeOutCtx, cancel := context.WithTimeout(ctx, timeout)
		log.Debugf("Running probe for array %s at time %v \n", systemID, time.Now())
		if existingStatus, ok := s.probeStatus.Load(systemID); !ok {
			log.Debugf("%s not in probeStatus ", systemID)
		} else {
			if status, ok = existingStatus.(ArrayConnectivityStatus); !ok {
				log.Errorf("failed to extract ArrayConnectivityStatus for array '%s'", systemID)
			}
		}
		// for the first time status will not be there.
		log.Debugf("array %s , status is %+v", systemID, status)
		// run nodeProbe to test connectivity
		err := s.requireProbe(timeOutCtx, systemID)
		if err == nil {
			log.Debugf("Probe successful for %s", systemID)
			status.LastSuccess = time.Now().Unix()
		} else {
			log.Warnf("Probe failed for array '%s' error:'%s'", systemID, err)
		}
		status.LastAttempt = time.Now().Unix()
		log.Debugf("array %s , storing status %+v", systemID, status)
		s.probeStatus.Store(systemID, status)
		cancel()
		// sleep for half the pollingFrequency and run check again
		time.Sleep(time.Second * time.Duration(atomic.LoadInt64(&pollingFrequencyInSeconds)/2))
	}
}

// SetPollingFrequency reads the pollingFrequency from Env, sets default vale if ENV not found
func SetPollingFrequency(ctx context.Context) int64 {
	var pollingFrequency int64
	if pollRateEnv, ok := csictx.LookupEnv(ctx, EnvPodmonArrayConnectivityPollRate); ok {
		if pollingFrequency, _ = strconv.ParseInt(pollRateEnv, 10, 32); pollingFrequency != 0 {
			log.Debugf("use pollingFrequency as %d seconds", pollingFrequency)
			return pollingFrequency
		}
	}
	log.Debugf("use default pollingFrequency as %d seconds", DefaultPodmonPollRate)
	return DefaultPodmonPollRate
}
