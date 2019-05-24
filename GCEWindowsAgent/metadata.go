//  Copyright 2017 Google Inc. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package main

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/guest-logging-go/logger"
)

const defaultEtag = "NONE"

var (
	metadataURL       = "http://metadata.google.internal/computeMetadata/v1/"
	metadataRecursive = "/?recursive=true&alt=json"
	metadataHang      = "&wait_for_change=true&timeout_sec=60"
	defaultTimeout    = 70 * time.Second
	etag              = defaultEtag
)

type metadataJSON struct {
	Instance instanceJSON
	Project  projectJSON
}

type instanceJSON struct {
	Attributes        attributesJSON
	NetworkInterfaces []networkInterfacesJSON
}

type networkInterfacesJSON struct {
	ForwardedIps      []string
	TargetInstanceIps []string
	Mac               string
}

type projectJSON struct {
	Attributes attributesJSON
	ProjectID  string `json:"projectId"`
}

type attributesJSON struct {
	WindowsKeys           windowsKeys
	Diagnostics           string
	DisableAddressManager *bool
	DisableAccountManager *bool
	EnableDiagnostics     *bool
	EnableWSFC            *bool
	WSFCAddresses         string
	WSFCAgentPort         string
}

type windowsKey struct {
	Email        string
	ExpireOn     string
	Exponent     string
	Modulus      string
	UserName     string
	HashFunction string
}

type windowsKeys []windowsKey

func (a *attributesJSON) UnmarshalJSON(b []byte) error {
	type inner struct {
		WindowsKeys           windowsKeys `json:"windows-keys"`
		Diagnostics           string      `json:"diagnostics"`
		DisableAddressManager string      `json:"disable-address-manager"`
		DisableAccountManager string      `json:"disable-account-manager"`
		EnableDiagnostics     string      `json:"enable-diagnostics"`
		EnableWSFC            string      `json:"enable-wsfc"`
		WSFCAddresses         string      `json:"wsfc-addrs"`
		WSFCAgentPort         string      `json:"wsfc-agent-port"`
	}
	var temp inner
	if err := json.Unmarshal(b, &temp); err != nil {
		return err
	}
	a.Diagnostics = temp.Diagnostics
	a.WSFCAddresses = temp.WSFCAddresses
	a.WSFCAgentPort = temp.WSFCAgentPort
	value, err := strconv.ParseBool(temp.DisableAddressManager)
	if err == nil {
		a.DisableAddressManager = &value
	}
	value, err = strconv.ParseBool(temp.DisableAccountManager)
	if err == nil {
		a.DisableAccountManager = &value
	}
	value, err = strconv.ParseBool(temp.EnableDiagnostics)
	if err == nil {
		a.EnableDiagnostics = &value
	}
	value, err = strconv.ParseBool(temp.EnableWSFC)
	if err == nil {
		a.EnableWSFC = &value
	}
	return nil
}

func (wks *windowsKeys) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	for _, jskey := range strings.Split(s, "\n") {
		var wk windowsKey
		if err := json.Unmarshal([]byte(jskey), &wk); err != nil {
			if !containsString(jskey, badKeys) {
				logger.Errorf("failed to unmarshal windows key from metadata: %s", err)
				badKeys = append(badKeys, jskey)
			}
			continue
		}
		if wk.Exponent != "" && wk.Modulus != "" && wk.UserName != "" && !wk.expired() {
			*wks = append(*wks, wk)
		}
	}
	return nil
}

func updateEtag(resp *http.Response) bool {
	oldEtag := etag
	etag = resp.Header.Get("etag")
	if etag == "" {
		etag = defaultEtag
	}
	return etag != oldEtag
}

func watchMetadata(ctx context.Context) (*metadataJSON, error) {
	return getMetadata(ctx, true)
}

func getMetadata(ctx context.Context, hang bool) (*metadataJSON, error) {
	client := &http.Client{
		Timeout: defaultTimeout,
	}

	finalURL := metadataURL + metadataRecursive
	if hang {
		finalURL += metadataHang
	}
	finalURL += ("&last_etag=" + etag)

	req, err := http.NewRequest("GET", finalURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Metadata-Flavor", "Google")
	req = req.WithContext(ctx)

	resp, err := client.Do(req)
	// Don't return error on a canceled context.
	if err != nil && ctx.Err() != nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// We return the response even if the etag has not been updated.
	if hang {
		updateEtag(resp)
	}

	md, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	var metadata metadataJSON
	return &metadata, json.Unmarshal(md, &metadata)
}
