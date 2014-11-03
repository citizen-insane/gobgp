package bgp

import (
	"bytes"
	"net"
	"testing"
)

func keepalive() *BGPMessage {
	return NewBGPKeepAliveMessage()
}

func notification() *BGPMessage {
	return NewBGPNotificationMessage(1, 2, nil)
}

func refresh() *BGPMessage {
	return NewBGPRouteRefreshMessage(1, 2, 10)
}

func open() *BGPMessage {
	p1 := NewOptionParameterCapability(
		[]ParameterCapabilityInterface{NewCapRouteRefresh()})
	p2 := NewOptionParameterCapability(
		[]ParameterCapabilityInterface{NewCapMultiProtocol(3, 4)})
	g := CapGracefulRestartTuples{4, 2, 3}
	p3 := NewOptionParameterCapability(
		[]ParameterCapabilityInterface{NewCapGracefulRestart(2, 100,
			[]CapGracefulRestartTuples{g})})
	p4 := NewOptionParameterCapability(
		[]ParameterCapabilityInterface{NewCapFourOctetASNumber(100000)})
	return NewBGPOpenMessage(11033, 303, "100.4.10.3",
		[]OptionParameterInterface{p1, p2, p3, p4})
}

func update() *BGPMessage {
	w1 := WithdrawnRoute{*NewIPAddrPrefix(23, "121.1.3.2")}
	w2 := WithdrawnRoute{*NewIPAddrPrefix(17, "100.33.3.0")}
	w := []WithdrawnRoute{w1, w2}

	aspath := []AsPathParam{
		AsPathParam{Type: 2, Num: 1, AS: []uint32{1000}},
		AsPathParam{Type: 1, Num: 2, AS: []uint32{1001, 1002}},
		AsPathParam{Type: 2, Num: 2, AS: []uint32{1003, 1004}},
	}

	aspath2 := []AsPathParam{
		AsPathParam{Type: 2, Num: 1, AS: []uint32{1000000}},
		AsPathParam{Type: 1, Num: 2, AS: []uint32{1000001, 1002}},
		AsPathParam{Type: 2, Num: 2, AS: []uint32{1003, 100004}},
	}

	ecommunities := []ExtendedCommunityInterface{
		&TwoOctetAsSpecificExtended{SubType: 1, AS: 10003, LocalAdmin: 3 << 20},
		&FourOctetAsSpecificExtended{SubType: 2, AS: 1 << 20, LocalAdmin: 300},
		&IPv4AddressSpecificExtended{SubType: 3, IPv4: net.ParseIP("192.2.1.2").To4(), LocalAdmin: 3000},
		&OpaqueExtended{Value: []byte{0, 1, 2, 3, 4, 5, 6, 7}},
		&UnknownExtended{Type: 99, Value: []byte{0, 1, 2, 3, 4, 5, 6, 7}},
	}

	mp_nlri := []AddrPrefixInterface{
		NewLabelledVPNIPAddrPrefix(20, "192.0.9.0", *NewLabel(1, 2, 3),
			NewRouteDistinguisherTwoOctetAS(256, 10000)),
		NewLabelledVPNIPAddrPrefix(26, "192.10.8.192", *NewLabel(5, 6, 7, 8),
			NewRouteDistinguisherIPAddressAS("10.0.1.1", 10001)),
	}

	mp_nlri2 := []AddrPrefixInterface{NewIPv6AddrPrefix(100,
		"fe80:1234:1234:5667:8967:af12:8912:1023")}

	mp_nlri3 := []AddrPrefixInterface{NewLabelledVPNIPv6AddrPrefix(100,
		"fe80:1234:1234:5667:8967:af12:1203:33a1", *NewLabel(5, 6),
		NewRouteDistinguisherFourOctetAS(5, 6))}

	mp_nlri4 := []AddrPrefixInterface{NewLabelledIPAddrPrefix(25, "192.168.0.0",
		*NewLabel(5, 6, 7))}

	p := []PathAttributeInterface{
		NewPathAttributeOrigin(3),
		NewPathAttributeAsPath(aspath),
		NewPathAttributeNextHop("129.1.1.2"),
		NewPathAttributeMultiExitDisc(1 << 20),
		NewPathAttributeLocalPref(1 << 22),
		NewPathAttributeAtomicAggregate(),
		NewPathAttributeAggregator(30002, "129.0.2.99"),
		NewPathAttributeCommunities([]uint32{1, 3}),
		NewPathAttributeOriginatorId("10.10.0.1"),
		NewPathAttributeClusterList([]string{"10.10.0.2", "10.10.0.3"}),
		NewPathAttributeExtendedCommunities(ecommunities),
		NewPathAttributeAs4Path(aspath2),
		NewPathAttributeAs4Aggregator(10000, "112.22.2.1"),
		NewPathAttributeMpReachNLRI("112.22.2.0", mp_nlri),
		NewPathAttributeMpReachNLRI("1023::", mp_nlri2),
		NewPathAttributeMpReachNLRI("fe80::", mp_nlri3),
		NewPathAttributeMpReachNLRI("129.1.1.1", mp_nlri4),
		NewPathAttributeMpUnreachNLRI(mp_nlri),
		&PathAttributeUnknown{
			PathAttribute: PathAttribute{
				Flags: 1,
				Type:  100,
				Value: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9},
			},
		},
	}
	n := []NLRInfo{*NewNLRInfo(24, "13.2.3.1")}
	return NewBGPUpdateMessage(w, p, n)
}

func Test_Message(t *testing.T) {
	l := []*BGPMessage{keepalive(), notification(), refresh(), open(), update()}
	for _, m := range l {
		buf1, _ := m.Serialize()
		t.Log("LEN =", len(buf1))
		msg, _ := ParseBGPMessage(buf1)
		buf2, _ := msg.Serialize()
		if bytes.Compare(buf1, buf2) == 0 {
			t.Log("OK")
		} else {
			t.Error("Something wrong")
			t.Error(len(buf1), &m, buf1)
			t.Error(len(buf2), &msg, buf2)
		}
	}
}