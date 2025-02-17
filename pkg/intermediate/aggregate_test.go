// Copyright 2021 VMware, Inc.
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

import (
	"bytes"
	"container/heap"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/vmware/go-ipfix/pkg/entities"
	"github.com/vmware/go-ipfix/pkg/registry"
	"github.com/vmware/go-ipfix/pkg/util"
)

var (
	fields = []string{
		"sourcePodName",
		"sourcePodNamespace",
		"sourceNodeName",
		"destinationPodName",
		"destinationPodNamespace",
		"destinationNodeName",
		"destinationClusterIPv4",
		"destinationClusterIPv6",
		"destinationServicePort",
		"ingressNetworkPolicyRuleAction",
		"egressNetworkPolicyRuleAction",
		"ingressNetworkPolicyRulePriority",
	}
	nonStatsElementList = []string{
		"flowEndSeconds",
		"flowEndReason",
		"tcpState",
	}
	statsElementList = []string{
		"packetTotalCount",
		"packetDeltaCount",
		"reversePacketTotalCount",
		"reversePacketDeltaCount",
	}
	antreaSourceStatsElementList = []string{
		"packetTotalCountFromSourceNode",
		"packetDeltaCountFromSourceNode",
		"reversePacketTotalCountFromSourceNode",
		"reversePacketDeltaCountFromSourceNode",
	}
	antreaDestinationStatsElementList = []string{
		"packetTotalCountFromDestinationNode",
		"packetDeltaCountFromDestinationNode",
		"reversePacketTotalCountFromDestinationNode",
		"reversePacketDeltaCountFromDestinationNode",
	}
)

func init() {
	registry.LoadRegistry()
	MaxRetries = 1
	MinExpiryTime = 0
}

const (
	testTemplateID     = uint16(256)
	testActiveExpiry   = 100 * time.Millisecond
	testInactiveExpiry = 150 * time.Millisecond
	testMaxRetries     = 2
)

func createMsgwithTemplateSet(isIPv6 bool) *entities.Message {
	set := entities.NewSet(true)
	set.PrepareSet(entities.Template, testTemplateID)
	elements := make([]*entities.InfoElementWithValue, 0)
	ie3 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourceTransportPort", 7, 2, 0, 2), nil)
	ie4 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationTransportPort", 11, 2, 0, 2), nil)
	ie5 := entities.NewInfoElementWithValue(entities.NewInfoElement("protocolIdentifier", 4, 1, 0, 1), nil)
	ie6 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourcePodName", 101, 13, registry.AntreaEnterpriseID, 65535), nil)
	ie7 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationPodName", 103, 13, registry.AntreaEnterpriseID, 65535), nil)
	ie9 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationServicePort", 107, 2, registry.AntreaEnterpriseID, 2), nil)
	var ie1, ie2, ie8 *entities.InfoElementWithValue
	if !isIPv6 {
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv4Address", 8, 18, 0, 4), nil)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv4Address", 12, 18, 0, 4), nil)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv4", 106, 18, registry.AntreaEnterpriseID, 4), nil)
	} else {
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv6Address", 8, 19, 0, 16), nil)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv6Address", 12, 19, 0, 16), nil)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv6", 106, 19, registry.AntreaEnterpriseID, 16), nil)
	}
	ie10 := entities.NewInfoElementWithValue(entities.NewInfoElement("flowEndSeconds", 151, 14, 0, 4), nil)
	ie11 := entities.NewInfoElementWithValue(entities.NewInfoElement("flowType", 137, 1, registry.AntreaEnterpriseID, 1), nil)
	ie12 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRuleAction", 139, 1, registry.AntreaEnterpriseID, 1), nil)
	ie13 := entities.NewInfoElementWithValue(entities.NewInfoElement("egressNetworkPolicyRuleAction", 140, 1, registry.AntreaEnterpriseID, 1), nil)
	ie14 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRulePriority", 116, 7, registry.AntreaEnterpriseID, 4), nil)

	elements = append(elements, ie1, ie2, ie3, ie4, ie5, ie6, ie7, ie8, ie9, ie10, ie11, ie12, ie13, ie14)
	set.AddRecord(elements, 256)

	message := entities.NewMessage(true)
	message.SetVersion(10)
	message.SetMessageLen(40)
	message.SetSequenceNum(1)
	message.SetObsDomainID(5678)
	message.SetExportTime(0)
	if isIPv6 {
		message.SetExportAddress("::1")
	} else {
		message.SetExportAddress("127.0.0.1")
	}
	message.AddSet(set)
	return message
}

// TODO:Cleanup this function using a loop, to make it easy to add elements for testing.
func createDataMsgForSrc(t *testing.T, isIPv6 bool, isIntraNode bool, isUpdatedRecord bool, isToExternal bool, isEgressDeny bool) *entities.Message {
	set := entities.NewSet(true)
	set.PrepareSet(entities.Data, testTemplateID)
	elements := make([]*entities.InfoElementWithValue, 0)
	srcPort := new(bytes.Buffer)
	dstPort := new(bytes.Buffer)
	proto := new(bytes.Buffer)
	svcPort := new(bytes.Buffer)
	srcPod := new(bytes.Buffer)
	dstPod := new(bytes.Buffer)
	srcAddr := new(bytes.Buffer)
	dstAddr := new(bytes.Buffer)
	svcAddr := new(bytes.Buffer)
	flowEndTime := new(bytes.Buffer)
	antreaFlowType := new(bytes.Buffer)
	flowEndReason := new(bytes.Buffer)
	tcpState := new(bytes.Buffer)
	ingressNetworkPolicyRuleAction := new(bytes.Buffer)
	egressNetworkPolicyRuleAction := new(bytes.Buffer)
	ingressNetworkPolicyRulePriority := new(bytes.Buffer)

	util.Encode(srcPort, binary.BigEndian, uint16(1234))
	util.Encode(dstPort, binary.BigEndian, uint16(5678))
	util.Encode(proto, binary.BigEndian, uint8(6))
	util.Encode(svcPort, binary.BigEndian, uint16(4739))
	util.Encode(ingressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionNoAction)
	if isEgressDeny {
		util.Encode(egressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionDrop)
	} else {
		util.Encode(egressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionNoAction)
	}

	srcPod.WriteString("pod1")
	if !isIntraNode {
		dstPod.WriteString("")
	} else {
		dstPod.WriteString("pod2")
	}
	ie3 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourceTransportPort", 7, 2, 0, 2), srcPort)
	ie4 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationTransportPort", 11, 2, 0, 2), dstPort)
	ie5 := entities.NewInfoElementWithValue(entities.NewInfoElement("protocolIdentifier", 4, 1, 0, 1), proto)
	ie6 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourcePodName", 101, 13, registry.AntreaEnterpriseID, 65535), srcPod)
	ie7 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationPodName", 103, 13, registry.AntreaEnterpriseID, 65535), dstPod)
	ie9 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationServicePort", 107, 2, registry.AntreaEnterpriseID, 2), svcPort)
	var ie1, ie2, ie8, ie11 *entities.InfoElementWithValue
	if !isIPv6 {
		util.Encode(srcAddr, binary.BigEndian, net.ParseIP("10.0.0.1").To4())
		util.Encode(dstAddr, binary.BigEndian, net.ParseIP("10.0.0.2").To4())
		util.Encode(svcAddr, binary.BigEndian, net.ParseIP("192.168.0.1").To4())
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv4Address", 8, 18, 0, 4), srcAddr)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv4Address", 12, 18, 0, 4), dstAddr)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv4", 106, 18, registry.AntreaEnterpriseID, 4), svcAddr)
	} else {
		util.Encode(srcAddr, binary.BigEndian, net.ParseIP("2001:0:3238:DFE1:63::FEFB"))
		util.Encode(dstAddr, binary.BigEndian, net.ParseIP("2001:0:3238:DFE1:63::FEFC"))
		util.Encode(svcAddr, binary.BigEndian, net.ParseIP("2001:0:3238:BBBB:63::AAAA"))
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv6Address", 8, 19, 0, 16), srcAddr)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv6Address", 12, 19, 0, 16), dstAddr)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv6", 106, 19, registry.AntreaEnterpriseID, 16), svcAddr)
	}

	if !isUpdatedRecord {
		util.Encode(flowEndTime, binary.BigEndian, uint32(1))
		util.Encode(flowEndReason, binary.BigEndian, registry.ActiveTimeoutReason)
		util.Encode(tcpState, binary.BigEndian, "ESTABLISHED")
	} else {
		util.Encode(flowEndTime, binary.BigEndian, uint32(10))
		util.Encode(flowEndReason, binary.BigEndian, registry.EndOfFlowReason)
		util.Encode(tcpState, binary.BigEndian, "TIME_WAIT")
	}
	tmpElement, _ := registry.GetInfoElement("flowEndSeconds", registry.IANAEnterpriseID)
	ie10 := entities.NewInfoElementWithValue(tmpElement, flowEndTime)
	if isToExternal {
		util.Encode(antreaFlowType, binary.BigEndian, registry.FlowTypeToExternal)
		util.Encode(ingressNetworkPolicyRulePriority, binary.BigEndian, int32(50000))
	} else if !isIntraNode {
		util.Encode(antreaFlowType, binary.BigEndian, registry.FlowTypeInterNode)
		util.Encode(ingressNetworkPolicyRulePriority, binary.BigEndian, int32(0))
	} else {
		util.Encode(antreaFlowType, binary.BigEndian, registry.FlowTypeIntraNode)
		util.Encode(ingressNetworkPolicyRulePriority, binary.BigEndian, int32(50000))
	}
	ie11 = entities.NewInfoElementWithValue(entities.NewInfoElement("flowType", 137, 1, registry.AntreaEnterpriseID, 1), antreaFlowType)
	tmpElement, _ = registry.GetInfoElement("flowEndReason", registry.IANAEnterpriseID)
	ie12 := entities.NewInfoElementWithValue(tmpElement, flowEndReason)
	tmpElement, _ = registry.GetInfoElement("tcpState", registry.AntreaEnterpriseID)
	ie13 := entities.NewInfoElementWithValue(tmpElement, tcpState)
	ie14 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRuleAction", 139, 1, registry.AntreaEnterpriseID, 1), ingressNetworkPolicyRuleAction)
	ie15 := entities.NewInfoElementWithValue(entities.NewInfoElement("egressNetworkPolicyRuleAction", 140, 1, registry.AntreaEnterpriseID, 1), egressNetworkPolicyRuleAction)
	ie16 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRulePriority", 116, 7, registry.AntreaEnterpriseID, 4), ingressNetworkPolicyRulePriority)

	elements = append(elements, ie1, ie2, ie3, ie4, ie5, ie6, ie7, ie8, ie9, ie10, ie11, ie12, ie13, ie14, ie15, ie16)
	// Add all elements in statsElements.
	for _, element := range statsElementList {
		var e *entities.InfoElement
		if !strings.Contains(element, "reverse") {
			e, _ = registry.GetInfoElement(element, registry.IANAEnterpriseID)
		} else {
			e, _ = registry.GetInfoElement(element, registry.IANAReversedEnterpriseID)
		}
		ieWithValue := entities.NewInfoElementWithValue(e, nil)
		value := new(bytes.Buffer)
		switch element {
		case "packetTotalCount", "reversePacketTotalCount":
			if !isUpdatedRecord {
				util.Encode(value, binary.BigEndian, uint64(500))
			} else {
				util.Encode(value, binary.BigEndian, uint64(1000))
			}
		case "packetDeltaCount", "reversePacketDeltaCount":
			if !isUpdatedRecord {
				util.Encode(value, binary.BigEndian, uint64(0))
			} else {
				util.Encode(value, binary.BigEndian, uint64(500))
			}
		}
		ieWithValue.Value = value
		elements = append(elements, ieWithValue)
	}

	err := set.AddRecord(elements, 256)
	assert.NoError(t, err)

	message := entities.NewMessage(true)
	message.SetVersion(10)
	message.SetMessageLen(32)
	message.SetSequenceNum(1)
	message.SetObsDomainID(1234)
	message.SetExportTime(0)
	if isIPv6 {
		message.SetExportAddress("::1")
	} else {
		message.SetExportAddress("127.0.0.1")
	}
	message.AddSet(set)

	return message
}

func createDataMsgForDst(t *testing.T, isIPv6 bool, isIntraNode bool, isUpdatedRecord bool, isIngressReject bool, isIngressDrop bool) *entities.Message {
	set := entities.NewSet(true)
	set.PrepareSet(entities.Data, testTemplateID)
	elements := make([]*entities.InfoElementWithValue, 0)
	srcPort := new(bytes.Buffer)
	dstPort := new(bytes.Buffer)
	proto := new(bytes.Buffer)
	svcPort := new(bytes.Buffer)
	srcPod := new(bytes.Buffer)
	dstPod := new(bytes.Buffer)
	srcAddr := new(bytes.Buffer)
	dstAddr := new(bytes.Buffer)
	svcAddr := new(bytes.Buffer)
	flowEndTime := new(bytes.Buffer)
	antreaFlowType := new(bytes.Buffer)
	flowEndReason := new(bytes.Buffer)
	tcpState := new(bytes.Buffer)
	ingressNetworkPolicyRuleAction := new(bytes.Buffer)
	egressNetworkPolicyRuleAction := new(bytes.Buffer)
	ingressNetworkPolicyRulePriority := new(bytes.Buffer)

	util.Encode(srcPort, binary.BigEndian, uint16(1234))
	util.Encode(dstPort, binary.BigEndian, uint16(5678))
	util.Encode(proto, binary.BigEndian, uint8(6))
	if isIngressReject {
		util.Encode(ingressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionReject)
	} else if isIngressDrop {
		util.Encode(ingressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionDrop)
	} else {
		util.Encode(ingressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionNoAction)
	}
	util.Encode(egressNetworkPolicyRuleAction, binary.BigEndian, registry.NetworkPolicyRuleActionNoAction)
	util.Encode(ingressNetworkPolicyRulePriority, binary.BigEndian, int32(50000))

	if !isIntraNode {
		util.Encode(svcPort, binary.BigEndian, uint16(0))
		srcPod.WriteString("")
	} else {
		util.Encode(svcPort, binary.BigEndian, uint16(4739))
		srcPod.WriteString("pod1")
	}
	dstPod.WriteString("pod2")
	ie3 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourceTransportPort", 7, 2, 0, 2), srcPort)
	ie4 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationTransportPort", 11, 2, 0, 2), dstPort)
	ie5 := entities.NewInfoElementWithValue(entities.NewInfoElement("protocolIdentifier", 4, 1, 0, 1), proto)
	ie6 := entities.NewInfoElementWithValue(entities.NewInfoElement("sourcePodName", 101, 13, registry.AntreaEnterpriseID, 65535), srcPod)
	ie7 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationPodName", 103, 13, registry.AntreaEnterpriseID, 65535), dstPod)
	ie9 := entities.NewInfoElementWithValue(entities.NewInfoElement("destinationServicePort", 107, 2, registry.AntreaEnterpriseID, 2), svcPort)
	var ie1, ie2, ie8, ie11 *entities.InfoElementWithValue
	if !isIPv6 {
		util.Encode(srcAddr, binary.BigEndian, net.ParseIP("10.0.0.1").To4())
		util.Encode(dstAddr, binary.BigEndian, net.ParseIP("10.0.0.2").To4())
		util.Encode(svcAddr, binary.BigEndian, net.ParseIP("0.0.0.0").To4())
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv4Address", 8, 18, 0, 4), srcAddr)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv4Address", 12, 18, 0, 4), dstAddr)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv4", 106, 18, registry.AntreaEnterpriseID, 4), svcAddr)
	} else {
		util.Encode(srcAddr, binary.BigEndian, net.ParseIP("2001:0:3238:DFE1:63::FEFB"))
		util.Encode(dstAddr, binary.BigEndian, net.ParseIP("2001:0:3238:DFE1:63::FEFC"))
		if !isIntraNode {
			util.Encode(svcAddr, binary.BigEndian, net.ParseIP("::0"))
		} else {
			util.Encode(svcAddr, binary.BigEndian, net.ParseIP("2001:0:3238:BBBB:63::AAAA"))
		}
		ie1 = entities.NewInfoElementWithValue(entities.NewInfoElement("sourceIPv6Address", 8, 19, 0, 16), srcAddr)
		ie2 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationIPv6Address", 12, 19, 0, 16), dstAddr)
		ie8 = entities.NewInfoElementWithValue(entities.NewInfoElement("destinationClusterIPv6", 106, 19, registry.AntreaEnterpriseID, 16), svcAddr)
	}
	if !isUpdatedRecord {
		util.Encode(flowEndTime, binary.BigEndian, uint32(1))
		util.Encode(flowEndReason, binary.BigEndian, registry.ActiveTimeoutReason)
		util.Encode(tcpState, binary.BigEndian, "ESTABLISHED")
	} else {
		util.Encode(flowEndTime, binary.BigEndian, uint32(10))
		util.Encode(flowEndReason, binary.BigEndian, registry.EndOfFlowReason)
		util.Encode(tcpState, binary.BigEndian, "TIME_WAIT")
	}
	tmpElement, _ := registry.GetInfoElement("flowEndSeconds", registry.IANAEnterpriseID)
	ie10 := entities.NewInfoElementWithValue(tmpElement, flowEndTime)
	if !isIntraNode {
		util.Encode(antreaFlowType, binary.BigEndian, registry.FlowTypeInterNode)
	} else {
		util.Encode(antreaFlowType, binary.BigEndian, registry.FlowTypeIntraNode)
	}
	ie11 = entities.NewInfoElementWithValue(entities.NewInfoElement("flowType", 137, 1, registry.AntreaEnterpriseID, 1), antreaFlowType)
	tmpElement, _ = registry.GetInfoElement("flowEndReason", registry.IANAEnterpriseID)
	ie12 := entities.NewInfoElementWithValue(tmpElement, flowEndReason)
	tmpElement, _ = registry.GetInfoElement("tcpState", registry.AntreaEnterpriseID)
	ie13 := entities.NewInfoElementWithValue(tmpElement, tcpState)
	ie14 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRuleAction", 139, 1, registry.AntreaEnterpriseID, 1), ingressNetworkPolicyRuleAction)
	ie15 := entities.NewInfoElementWithValue(entities.NewInfoElement("egressNetworkPolicyRuleAction", 140, 1, registry.AntreaEnterpriseID, 1), egressNetworkPolicyRuleAction)
	ie16 := entities.NewInfoElementWithValue(entities.NewInfoElement("ingressNetworkPolicyRulePriority", 116, 7, registry.AntreaEnterpriseID, 4), ingressNetworkPolicyRulePriority)

	elements = append(elements, ie1, ie2, ie3, ie4, ie5, ie6, ie7, ie8, ie9, ie10, ie11, ie12, ie13, ie14, ie15, ie16)
	// Add all elements in statsElements.
	for _, element := range statsElementList {
		var e *entities.InfoElement
		if !strings.Contains(element, "reverse") {
			e, _ = registry.GetInfoElement(element, registry.IANAEnterpriseID)
		} else {
			e, _ = registry.GetInfoElement(element, registry.IANAReversedEnterpriseID)
		}
		ieWithValue := entities.NewInfoElementWithValue(e, nil)
		value := new(bytes.Buffer)
		switch element {
		case "packetTotalCount", "reversePacketTotalCount":
			if !isUpdatedRecord {
				util.Encode(value, binary.BigEndian, uint64(502))
			} else {
				util.Encode(value, binary.BigEndian, uint64(1005))
			}
		case "packetDeltaCount", "reversePacketDeltaCount":
			if !isUpdatedRecord {
				util.Encode(value, binary.BigEndian, uint64(0))
			} else {
				util.Encode(value, binary.BigEndian, uint64(503))
			}
		}
		ieWithValue.Value = value
		elements = append(elements, ieWithValue)
	}
	err := set.AddRecord(elements, 256)
	assert.NoError(t, err)

	message := entities.NewMessage(true)
	message.SetVersion(10)
	message.SetMessageLen(32)
	message.SetSequenceNum(1)
	message.SetObsDomainID(1234)
	message.SetExportTime(0)
	if isIPv6 {
		message.SetExportAddress("::1")
	} else {
		message.SetExportAddress("127.0.0.1")
	}
	message.AddSet(set)

	return message
}

func TestInitAggregationProcess(t *testing.T) {
	input := AggregationInput{
		MessageChan:     nil,
		WorkerNum:       2,
		CorrelateFields: fields,
	}
	aggregationProcess, err := InitAggregationProcess(input)
	assert.NotNil(t, err)
	assert.Nil(t, aggregationProcess)
	messageChan := make(chan *entities.Message)
	input.MessageChan = messageChan
	aggregationProcess, err = InitAggregationProcess(input)
	assert.Nil(t, err)
	assert.Equal(t, 2, aggregationProcess.workerNum)
}

func TestGetTupleRecordMap(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:     messageChan,
		WorkerNum:       2,
		CorrelateFields: fields,
	}
	aggregationProcess, _ := InitAggregationProcess(input)
	assert.Equal(t, aggregationProcess.flowKeyRecordMap, aggregationProcess.flowKeyRecordMap)
}

func TestAggregateMsgByFlowKey(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	aggregationProcess, _ := InitAggregationProcess(input)
	// Template records with IPv4 fields should be ignored
	message := createMsgwithTemplateSet(false)
	err := aggregationProcess.AggregateMsgByFlowKey(message)
	assert.NoError(t, err)
	assert.Empty(t, aggregationProcess.flowKeyRecordMap)
	assert.Empty(t, aggregationProcess.expirePriorityQueue.Len())
	// Data records should be processed and stored with corresponding flow key
	message = createDataMsgForSrc(t, false, false, false, false, false)
	err = aggregationProcess.AggregateMsgByFlowKey(message)
	assert.NoError(t, err)
	assert.NotZero(t, len(aggregationProcess.flowKeyRecordMap))
	assert.NotZero(t, aggregationProcess.expirePriorityQueue.Len())
	flowKey := FlowKey{"10.0.0.1", "10.0.0.2", 6, 1234, 5678}
	aggRecord := aggregationProcess.flowKeyRecordMap[flowKey]
	assert.NotNil(t, aggregationProcess.flowKeyRecordMap[flowKey])
	item := aggregationProcess.expirePriorityQueue.Peek()
	assert.NotNil(t, item)
	ieWithValue, exist := aggRecord.Record.GetInfoElementWithValue("sourceIPv4Address")
	assert.Equal(t, true, exist)
	assert.Equal(t, net.IP{0xa, 0x0, 0x0, 0x1}, ieWithValue.Value)
	assert.Equal(t, message.GetSet().GetRecords()[0], aggRecord.Record)

	// Template records with IPv6 fields should be ignored
	message = createMsgwithTemplateSet(true)
	err = aggregationProcess.AggregateMsgByFlowKey(message)
	assert.NoError(t, err)
	// It should have only data record with IPv4 fields that is added before.
	assert.Equal(t, 1, len(aggregationProcess.flowKeyRecordMap))
	assert.Equal(t, 1, aggregationProcess.expirePriorityQueue.Len())
	// Data record with IPv6 addresses should be processed and stored correctly
	message = createDataMsgForSrc(t, true, false, false, false, false)
	err = aggregationProcess.AggregateMsgByFlowKey(message)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(aggregationProcess.flowKeyRecordMap))
	assert.Equal(t, 2, aggregationProcess.expirePriorityQueue.Len())
	flowKey = FlowKey{"2001:0:3238:dfe1:63::fefb", "2001:0:3238:dfe1:63::fefc", 6, 1234, 5678}
	assert.NotNil(t, aggregationProcess.flowKeyRecordMap[flowKey])
	aggRecord = aggregationProcess.flowKeyRecordMap[flowKey]
	ieWithValue, exist = aggRecord.Record.GetInfoElementWithValue("sourceIPv6Address")
	assert.Equal(t, true, exist)
	assert.Equal(t, net.IP{0x20, 0x1, 0x0, 0x0, 0x32, 0x38, 0xdf, 0xe1, 0x0, 0x63, 0x0, 0x0, 0x0, 0x0, 0xfe, 0xfb}, ieWithValue.Value)
	assert.Equal(t, message.GetSet().GetRecords()[0], aggRecord.Record)

	// Test data record with invalid "flowEndSeconds" field
	element, _ := message.GetSet().GetRecords()[0].GetInfoElementWithValue("flowEndSeconds")
	element.Value = nil
	err = aggregationProcess.AggregateMsgByFlowKey(message)
	assert.Error(t, err)
}

func TestAggregationProcess(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:     messageChan,
		WorkerNum:       2,
		CorrelateFields: fields,
	}
	aggregationProcess, _ := InitAggregationProcess(input)
	dataMsg := createDataMsgForSrc(t, false, false, false, false, false)
	go func() {
		messageChan <- createMsgwithTemplateSet(false)
		time.Sleep(time.Second)
		messageChan <- dataMsg
		time.Sleep(time.Second)
		close(messageChan)
		aggregationProcess.Stop()
	}()
	// the Start() function is blocking until above goroutine with Stop() finishes
	// Proper usage of aggregation process is to have Start() in a goroutine with external channel
	aggregationProcess.Start()
	flowKey := FlowKey{
		"10.0.0.1", "10.0.0.2", 6, 1234, 5678,
	}
	aggRecord := aggregationProcess.flowKeyRecordMap[flowKey]
	assert.Equalf(t, aggRecord.Record, dataMsg.GetSet().GetRecords()[0], "records should be equal")
}

func TestAddOriginalExporterInfo(t *testing.T) {
	// Test message with template set
	message := createMsgwithTemplateSet(false)
	err := addOriginalExporterInfo(message)
	assert.NoError(t, err)
	record := message.GetSet().GetRecords()[0]
	_, exist := record.GetInfoElementWithValue("originalExporterIPv4Address")
	assert.Equal(t, true, exist)
	_, exist = record.GetInfoElementWithValue("originalObservationDomainId")
	assert.Equal(t, true, exist)
	// Test message with data set
	message = createDataMsgForSrc(t, false, false, false, false, false)
	err = addOriginalExporterInfo(message)
	assert.NoError(t, err)
	record = message.GetSet().GetRecords()[0]
	ieWithValue, exist := record.GetInfoElementWithValue("originalExporterIPv4Address")
	assert.Equal(t, true, exist)
	assert.Equal(t, net.IP{0x7f, 0x0, 0x0, 0x1}, ieWithValue.Value)
	ieWithValue, exist = record.GetInfoElementWithValue("originalObservationDomainId")
	assert.Equal(t, true, exist)
	assert.Equal(t, uint32(1234), ieWithValue.Value)
}

func TestAddOriginalExporterInfoIPv6(t *testing.T) {
	// Test message with template set
	message := createMsgwithTemplateSet(true)
	err := addOriginalExporterInfo(message)
	assert.NoError(t, err)
	record := message.GetSet().GetRecords()[0]
	_, exist := record.GetInfoElementWithValue("originalExporterIPv6Address")
	assert.Equal(t, true, exist)
	_, exist = record.GetInfoElementWithValue("originalObservationDomainId")
	assert.Equal(t, true, exist)
	// Test message with data set
	message = createDataMsgForSrc(t, true, false, false, false, false)
	err = addOriginalExporterInfo(message)
	assert.NoError(t, err)
	record = message.GetSet().GetRecords()[0]
	ieWithValue, exist := record.GetInfoElementWithValue("originalExporterIPv6Address")
	assert.Equal(t, true, exist)
	assert.Equal(t, net.IP{0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1}, ieWithValue.Value)
	ieWithValue, exist = record.GetInfoElementWithValue("originalObservationDomainId")
	assert.Equal(t, true, exist)
	assert.Equal(t, uint32(1234), ieWithValue.Value)
}

func TestCorrelateRecordsForInterNodeFlow(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)
	// Test IPv4 fields.
	// Test the scenario, where record1 is added first and then record2.
	record1 := createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	record2 := createDataMsgForDst(t, false, false, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, record2, false, false, true)
	// Cleanup the flowKeyMap in aggregation process.
	flowKey1, _ := getFlowKeyFromRecord(record1)
	err := ap.deleteFlowKeyFromMap(*flowKey1)
	assert.NoError(t, err)
	heap.Pop(&ap.expirePriorityQueue)
	// Test the scenario, where record2 is added first and then record1.
	record1 = createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	record2 = createDataMsgForDst(t, false, false, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record2, record1, false, false, true)
	// Cleanup the flowKeyMap in aggregation process.
	err = ap.deleteFlowKeyFromMap(*flowKey1)
	assert.NoError(t, err)
	heap.Pop(&ap.expirePriorityQueue)
	// Test IPv6 fields.
	// Test the scenario, where record1 is added first and then record2.
	record1 = createDataMsgForSrc(t, true, false, false, false, false).GetSet().GetRecords()[0]
	record2 = createDataMsgForDst(t, true, false, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, record2, true, false, true)
	// Cleanup the flowKeyMap in aggregation process.
	flowKey1, _ = getFlowKeyFromRecord(record1)
	err = ap.deleteFlowKeyFromMap(*flowKey1)
	assert.NoError(t, err)
	heap.Pop(&ap.expirePriorityQueue)
	// Test the scenario, where record2 is added first and then record1.
	record1 = createDataMsgForSrc(t, true, false, false, false, false).GetSet().GetRecords()[0]
	record2 = createDataMsgForDst(t, true, false, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record2, record1, true, false, true)
}

func TestCorrelateRecordsForInterNodeDenyFlow(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:     messageChan,
		WorkerNum:       2,
		CorrelateFields: fields,
	}
	ap, _ := InitAggregationProcess(input)
	// Test the scenario, where src record has egress deny rule
	record1 := createDataMsgForSrc(t, false, false, false, false, true).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, nil, false, false, false)
	// Cleanup the flowKeyMap in aggregation process.
	flowKey1, _ := getFlowKeyFromRecord(record1)
	ap.deleteFlowKeyFromMap(*flowKey1)
	heap.Pop(&ap.expirePriorityQueue)
	// Test the scenario, where dst record has ingress reject rule
	record2 := createDataMsgForDst(t, false, false, false, true, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record2, nil, false, false, false)
	// Cleanup the flowKeyMap in aggregation process.
	ap.deleteFlowKeyFromMap(*flowKey1)
	heap.Pop(&ap.expirePriorityQueue)
	// Test the scenario, where dst record has ingress drop rule
	record1 = createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	record2 = createDataMsgForDst(t, false, false, false, false, true).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, record2, false, false, true)
	// Cleanup the flowKeyMap in aggregation process.
	ap.deleteFlowKeyFromMap(*flowKey1)

}

func TestCorrelateRecordsForIntraNodeFlow(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)
	// Test IPv4 fields.
	record1 := createDataMsgForSrc(t, false, true, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, nil, false, true, false)
	// Cleanup the flowKeyMap in aggregation process.
	flowKey1, _ := getFlowKeyFromRecord(record1)
	err := ap.deleteFlowKeyFromMap(*flowKey1)
	assert.NoError(t, err)
	heap.Pop(&ap.expirePriorityQueue)
	// Test IPv6 fields.
	record1 = createDataMsgForSrc(t, true, true, false, false, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, nil, true, true, false)
}

func TestCorrelateRecordsForToExternalFlow(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)
	// Test IPv4 fields.
	record1 := createDataMsgForSrc(t, false, true, false, true, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, nil, false, true, false)
	// Cleanup the flowKeyMap in aggregation process.
	flowKey1, _ := getFlowKeyFromRecord(record1)
	err := ap.deleteFlowKeyFromMap(*flowKey1)
	assert.NoError(t, err)
	heap.Pop(&ap.expirePriorityQueue)
	// Test IPv6 fields.
	record1 = createDataMsgForSrc(t, true, true, false, true, false).GetSet().GetRecords()[0]
	runCorrelationAndCheckResult(t, ap, record1, nil, true, true, false)
}

func TestAggregateRecordsForInterNodeFlow(t *testing.T) {
	messageChan := make(chan *entities.Message)
	aggElements := &AggregationElements{
		NonStatsElements:                   nonStatsElementList,
		StatsElements:                      statsElementList,
		AggregatedSourceStatsElements:      antreaSourceStatsElementList,
		AggregatedDestinationStatsElements: antreaDestinationStatsElementList,
	}
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		AggregateElements:     aggElements,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)

	// Test the scenario (added in order): srcRecord, dstRecord, record1_updated, record2_updated
	srcRecord := createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	dstRecord := createDataMsgForDst(t, false, false, false, false, false).GetSet().GetRecords()[0]
	latestSrcRecord := createDataMsgForSrc(t, false, false, true, false, false).GetSet().GetRecords()[0]
	latestDstRecord := createDataMsgForDst(t, false, false, true, false, false).GetSet().GetRecords()[0]
	runAggregationAndCheckResult(t, ap, srcRecord, dstRecord, latestSrcRecord, latestDstRecord, false)
}

func TestDeleteFlowKeyFromMapWithLock(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:     messageChan,
		WorkerNum:       2,
		CorrelateFields: fields,
	}
	aggregationProcess, _ := InitAggregationProcess(input)
	message := createDataMsgForSrc(t, false, false, false, false, false)
	flowKey1 := FlowKey{"10.0.0.1", "10.0.0.2", 6, 1234, 5678}
	flowKey2 := FlowKey{"2001:0:3238:dfe1:63::fefb", "2001:0:3238:dfe1:63::fefc", 6, 1234, 5678}
	aggFlowRecord := AggregationFlowRecord{
		message.GetSet().GetRecords()[0],
		&ItemToExpire{},
		true,
		0,
	}
	aggregationProcess.flowKeyRecordMap[flowKey1] = aggFlowRecord
	assert.Equal(t, 1, len(aggregationProcess.flowKeyRecordMap))
	err := aggregationProcess.deleteFlowKeyFromMap(flowKey2)
	assert.Error(t, err)
	assert.Equal(t, 1, len(aggregationProcess.flowKeyRecordMap))
	err = aggregationProcess.deleteFlowKeyFromMap(flowKey1)
	assert.NoError(t, err)
	assert.Empty(t, aggregationProcess.flowKeyRecordMap)
}

func TestGetExpiryFromExpirePriorityQueue(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)
	// Add records with IPv4 fields.
	recordIPv4Src := createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	recordIPv4Dst := createDataMsgForDst(t, false, false, false, false, false).GetSet().GetRecords()[0]
	// Add records with IPv6 fields.
	recordIPv6Src := createDataMsgForSrc(t, true, false, false, false, false).GetSet().GetRecords()[0]
	recordIPv6Dst := createDataMsgForDst(t, true, false, false, false, false).GetSet().GetRecords()[0]
	testCases := []struct {
		name    string
		records []entities.Record
	}{
		{
			"empty queue",
			nil,
		},
		{
			"One aggregation record",
			[]entities.Record{recordIPv4Src, recordIPv4Dst},
		},
		{
			"Two aggregation records",
			[]entities.Record{recordIPv4Src, recordIPv4Dst, recordIPv6Src, recordIPv6Dst},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			for _, record := range tc.records {
				flowKey, _ := getFlowKeyFromRecord(record)
				err := ap.addOrUpdateRecordInMap(flowKey, record)
				assert.NoError(t, err)
			}
			expiryTime := ap.GetExpiryFromExpirePriorityQueue()
			assert.LessOrEqualf(t, expiryTime.Nanoseconds(), testActiveExpiry.Nanoseconds(), "incorrect expiry time")
		})
	}
}

func TestForAllExpiredFlowRecordsDo(t *testing.T) {
	messageChan := make(chan *entities.Message)
	input := AggregationInput{
		MessageChan:           messageChan,
		WorkerNum:             2,
		CorrelateFields:       fields,
		ActiveExpiryTimeout:   testActiveExpiry,
		InactiveExpiryTimeout: testInactiveExpiry,
	}
	ap, _ := InitAggregationProcess(input)
	// Add records with IPv4 fields.
	recordIPv4Src := createDataMsgForSrc(t, false, false, false, false, false).GetSet().GetRecords()[0]
	recordIPv4Dst := createDataMsgForDst(t, false, false, false, false, false).GetSet().GetRecords()[0]
	// Add records with IPv6 fields.
	recordIPv6Src := createDataMsgForSrc(t, true, false, false, false, false).GetSet().GetRecords()[0]
	recordIPv6Dst := createDataMsgForDst(t, true, false, false, false, false).GetSet().GetRecords()[0]
	numExecutions := 0
	testCallback := func(key FlowKey, record AggregationFlowRecord) error {
		numExecutions = numExecutions + 1
		return nil
	}

	testCases := []struct {
		name               string
		records            []entities.Record
		expectedExecutions int
		expectedPQLen      int
	}{
		{
			"empty queue",
			nil,
			0,
			0,
		},
		{
			"One aggregation record and none expired",
			[]entities.Record{recordIPv4Src, recordIPv4Dst},
			0,
			1,
		},
		{
			"One aggregation record and one expired",
			[]entities.Record{recordIPv4Src, recordIPv4Dst},
			1,
			1,
		},
		{
			"Two aggregation records and one expired",
			[]entities.Record{recordIPv4Src, recordIPv4Dst, recordIPv6Src, recordIPv6Dst},
			1,
			2,
		},
		{
			"Two aggregation records and two expired",
			[]entities.Record{recordIPv4Src, recordIPv4Dst, recordIPv6Src, recordIPv6Dst},
			2,
			0,
		},
		{
			"One aggregation record and waitForReadyToSendRetries reach maximum",
			[]entities.Record{recordIPv4Src},
			0,
			0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			numExecutions = 0
			for _, record := range tc.records {
				flowKey, _ := getFlowKeyFromRecord(record)
				err := ap.addOrUpdateRecordInMap(flowKey, record)
				assert.NoError(t, err)
			}
			switch tc.name {
			case "One aggregation record and one expired":
				time.Sleep(testActiveExpiry)
				err := ap.ForAllExpiredFlowRecordsDo(testCallback)
				assert.NoError(t, err)
			case "Two aggregation records and one expired":
				time.Sleep(testActiveExpiry)
				secondAggRec := ap.expirePriorityQueue[1]
				ap.expirePriorityQueue.Update(secondAggRec, secondAggRec.flowKey,
					secondAggRec.flowRecord, secondAggRec.activeExpireTime.Add(testActiveExpiry), secondAggRec.inactiveExpireTime.Add(testInactiveExpiry))
				err := ap.ForAllExpiredFlowRecordsDo(testCallback)
				assert.NoError(t, err)
			case "Two aggregation records and two expired":
				time.Sleep(2 * testActiveExpiry)
				err := ap.ForAllExpiredFlowRecordsDo(testCallback)
				assert.NoError(t, err)
			case "One aggregation record and waitForReadyToSendRetries reach maximum":
				for i := 0; i < testMaxRetries; i++ {
					time.Sleep(testActiveExpiry)
					err := ap.ForAllExpiredFlowRecordsDo(testCallback)
					assert.NoError(t, err)
				}
			default:
				break
			}
			assert.Equalf(t, tc.expectedExecutions, numExecutions, "number of callback executions are incorrect")
			assert.Equalf(t, tc.expectedPQLen, ap.expirePriorityQueue.Len(), "expected pq length not correct")
		})
	}
}

func runCorrelationAndCheckResult(t *testing.T, ap *AggregationProcess, record1, record2 entities.Record, isIPv6, isIntraNode, needsCorrleation bool) {
	flowKey1, _ := getFlowKeyFromRecord(record1)
	err := ap.addOrUpdateRecordInMap(flowKey1, record1)
	assert.NoError(t, err)
	item := ap.expirePriorityQueue.Peek()
	oldActiveExpiryTime := item.activeExpireTime
	oldInactiveExpiryTime := item.inactiveExpireTime
	if !isIntraNode && needsCorrleation {
		flowKey2, _ := getFlowKeyFromRecord(record2)
		assert.Equalf(t, *flowKey1, *flowKey2, "flow keys should be equal.")
		err = ap.addOrUpdateRecordInMap(flowKey2, record2)
		assert.NoError(t, err)
	}
	assert.Equal(t, 1, len(ap.flowKeyRecordMap))
	assert.Equal(t, 1, ap.expirePriorityQueue.Len())
	aggRecord, _ := ap.flowKeyRecordMap[*flowKey1]
	item = ap.expirePriorityQueue.Peek()
	assert.Equal(t, aggRecord, *item.flowRecord)
	assert.Equal(t, oldActiveExpiryTime, item.activeExpireTime)
	if !isIntraNode && needsCorrleation {
		assert.NotEqual(t, oldInactiveExpiryTime, item.inactiveExpireTime)
	}
	if !isIntraNode && !needsCorrleation {
		// for inter-Node deny connections, either src or dst Pod info will be resolved.
		sourcePodName, _ := aggRecord.Record.GetInfoElementWithValue("sourcePodName")
		destinationPodName, _ := aggRecord.Record.GetInfoElementWithValue("destinationPodName")
		assert.True(t, sourcePodName.Value == "" || destinationPodName.Value == "")
		egress, _ := aggRecord.Record.GetInfoElementWithValue("egressNetworkPolicyRuleAction")
		ingress, _ := aggRecord.Record.GetInfoElementWithValue("ingressNetworkPolicyRuleAction")
		assert.True(t, egress.Value != 0 || ingress.Value != 0)
	} else {
		ieWithValue, _ := aggRecord.Record.GetInfoElementWithValue("sourcePodName")
		assert.Equal(t, "pod1", ieWithValue.Value)
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationPodName")
		assert.Equal(t, "pod2", ieWithValue.Value)
		if !isIPv6 {
			ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationClusterIPv4")
			assert.Equal(t, net.ParseIP("192.168.0.1").To4(), ieWithValue.Value)
		} else {
			ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationClusterIPv6")
			assert.Equal(t, net.ParseIP("2001:0:3238:BBBB:63::AAAA"), ieWithValue.Value)
		}
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationServicePort")
		assert.Equal(t, uint16(4739), ieWithValue.Value)
		ingressPriority, _ := aggRecord.Record.GetInfoElementWithValue("ingressNetworkPolicyRulePriority")
		assert.Equal(t, ingressPriority.Value, int32(50000))
	}
}

func runAggregationAndCheckResult(t *testing.T, ap *AggregationProcess, srcRecord, dstRecord, srcRecordLatest, dstRecordLatest entities.Record, isIntraNode bool) {
	flowKey, _ := getFlowKeyFromRecord(srcRecord)
	err := ap.addOrUpdateRecordInMap(flowKey, srcRecord)
	assert.NoError(t, err)
	item := ap.expirePriorityQueue.Peek()
	oldActiveExpiryTime := item.activeExpireTime
	oldInactiveExpiryTime := item.inactiveExpireTime

	if !isIntraNode {
		err = ap.addOrUpdateRecordInMap(flowKey, dstRecord)
		assert.NoError(t, err)
	}
	err = ap.addOrUpdateRecordInMap(flowKey, srcRecordLatest)
	assert.NoError(t, err)
	if !isIntraNode {
		err = ap.addOrUpdateRecordInMap(flowKey, dstRecordLatest)
		assert.NoError(t, err)
	}
	assert.Equal(t, 1, len(ap.flowKeyRecordMap))
	assert.Equal(t, 1, ap.expirePriorityQueue.Len())
	aggRecord, _ := ap.flowKeyRecordMap[*flowKey]
	item = ap.expirePriorityQueue.Peek()
	assert.Equal(t, aggRecord, *item.flowRecord)
	assert.Equal(t, oldActiveExpiryTime, item.activeExpireTime)
	if !isIntraNode {
		assert.NotEqual(t, oldInactiveExpiryTime, item.inactiveExpireTime)
	}
	ieWithValue, _ := aggRecord.Record.GetInfoElementWithValue("sourcePodName")
	assert.Equal(t, "pod1", ieWithValue.Value)
	ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationPodName")
	assert.Equal(t, "pod2", ieWithValue.Value)
	ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationClusterIPv4")
	assert.Equal(t, net.ParseIP("192.168.0.1").To4(), ieWithValue.Value)
	ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("destinationServicePort")
	assert.Equal(t, uint16(4739), ieWithValue.Value)
	ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue("ingressNetworkPolicyRuleAction")
	assert.Equal(t, registry.NetworkPolicyRuleActionNoAction, ieWithValue.Value)
	for _, e := range nonStatsElementList {
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue(e)
		expectedIE, _ := dstRecordLatest.GetInfoElementWithValue(e)
		assert.Equal(t, expectedIE.Value, ieWithValue.Value)
	}
	for _, e := range statsElementList {
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue(e)
		latestRecord, _ := dstRecordLatest.GetInfoElementWithValue(e)
		if !strings.Contains(e, "Delta") {
			assert.Equalf(t, latestRecord.Value, ieWithValue.Value, "values should be equal for element %v", e)
		} else {
			prevRecord, _ := srcRecordLatest.GetInfoElementWithValue(e)
			assert.Equalf(t, prevRecord.Value.(uint64)+latestRecord.Value.(uint64), ieWithValue.Value, "values should be equal for element %v", e)
		}
	}
	for i, e := range antreaSourceStatsElementList {
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue(e)
		latestRecord, _ := srcRecordLatest.GetInfoElementWithValue(statsElementList[i])
		assert.Equalf(t, latestRecord.Value, ieWithValue.Value, "values should be equal for element %v", e)
	}
	for i, e := range antreaDestinationStatsElementList {
		ieWithValue, _ = aggRecord.Record.GetInfoElementWithValue(e)
		latestRecord, _ := dstRecordLatest.GetInfoElementWithValue(statsElementList[i])
		assert.Equalf(t, latestRecord.Value, ieWithValue.Value, "values should be equal for element %v", e)
	}
}
