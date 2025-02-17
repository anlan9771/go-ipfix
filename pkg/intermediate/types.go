// Copyright 2020 VMware, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package intermediate

import "github.com/vmware/go-ipfix/pkg/entities"

type FlowKey struct {
	SourceAddress      string
	DestinationAddress string
	Protocol           uint8
	SourcePort         uint16
	DestinationPort    uint16
}

type AggregationFlowRecord struct {
	Record entities.Record
	// Flow record contains mapping to its reference in priority queue.
	PriorityQueueItem *ItemToExpire
	// ReadyToSend is an indicator that we received all required records for the
	// given flow, i.e., records from source and destination nodes for the case
	// inter-node flow and record from the node for the case of intra-node flow.
	ReadyToSend               bool
	waitForReadyToSendRetries int
}

type AggregationElements struct {
	NonStatsElements                   []string
	StatsElements                      []string
	AggregatedSourceStatsElements      []string
	AggregatedDestinationStatsElements []string
}

type FlowKeyRecordMapCallBack func(key FlowKey, record AggregationFlowRecord) error
