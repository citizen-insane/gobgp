// Copyright (C) 2015-2016 Nippon Telegraph and Telephone Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/citizen-insane/gobgp/config"
	"github.com/citizen-insane/gobgp/packet/bgp"
	"github.com/citizen-insane/gobgp/packet/bmp"
	"github.com/citizen-insane/gobgp/table"
	"net"
	"strconv"
	"time"
)

type ribout map[string][]*table.Path

func newribout() ribout {
	return make(map[string][]*table.Path)
}

// return true if we need to send the path to the BMP server
func (r ribout) update(p *table.Path) bool {
	key := p.GetNlri().String() // TODO expose (*Path).getPrefix()
	l := r[key]
	if p.IsWithdraw {
		if len(l) == 0 {
			return false
		}
		n := make([]*table.Path, 0, len(l))
		for _, q := range l {
			if p.GetSource() == q.GetSource() {
				continue
			}
			n = append(n, q)
		}
		if len(n) == 0 {
			delete(r, key)
		} else {
			r[key] = n
		}
		return true
	}

	if len(l) == 0 {
		r[key] = []*table.Path{p}
		return true
	}

	doAppend := true
	for idx, q := range l {
		if p.GetSource() == q.GetSource() {
			// if we have sent the same path, don't send it again
			if p.Equal(q) {
				return false
			}
			l[idx] = p
			doAppend = false
		}
	}
	if doAppend {
		r[key] = append(r[key], p)
	}
	return true
}

func (b *bmpClient) tryConnect() *net.TCPConn {
	interval := 1
	for {
		log.WithFields(log.Fields{"Topic": "bmp"}).Debugf("Connecting BMP server:%s", b.host)
		conn, err := net.Dial("tcp", b.host)
		if err != nil {
			select {
			case <-b.dead:
				return nil
			default:
			}
			time.Sleep(time.Duration(interval) * time.Second)
			if interval < 30 {
				interval *= 2
			}
		} else {
			log.WithFields(log.Fields{"Topic": "bmp"}).Infof("BMP server is connected:%s", b.host)
			return conn.(*net.TCPConn)
		}
	}
}

func (b *bmpClient) Stop() {
	close(b.dead)
}

func (b *bmpClient) loop() {
	for {
		conn := b.tryConnect()
		if conn == nil {
			break
		}

		if func() bool {
			ops := []WatchOption{WatchPeerState(true)}
			if b.typ != config.BMP_ROUTE_MONITORING_POLICY_TYPE_POST_POLICY {
				ops = append(ops, WatchUpdate(true))
			} else if b.typ != config.BMP_ROUTE_MONITORING_POLICY_TYPE_PRE_POLICY {
				ops = append(ops, WatchPostUpdate(true))
			}
			w := b.s.Watch(ops...)
			defer w.Stop()

			write := func(msg *bmp.BMPMessage) error {
				buf, _ := msg.Serialize()
				_, err := conn.Write(buf)
				if err != nil {
					log.Warnf("failed to write to bmp server %s", b.host)
				}
				return err
			}

			if err := write(bmp.NewBMPInitiation([]bmp.BMPTLV{})); err != nil {
				return false
			}

			for {
				select {
				case ev := <-w.Event():
					switch msg := ev.(type) {
					case *WatchEventUpdate:
						info := &table.PeerInfo{
							Address: msg.PeerAddress,
							AS:      msg.PeerAS,
							ID:      msg.PeerID,
						}
						if msg.Payload == nil {
							pathList := make([]*table.Path, 0, len(msg.PathList))
							for _, p := range msg.PathList {
								if b.ribout.update(p) {
									pathList = append(pathList, p)
								}
							}
							for _, u := range table.CreateUpdateMsgFromPaths(pathList) {
								payload, _ := u.Serialize()
								if err := write(bmpPeerRoute(bmp.BMP_PEER_TYPE_GLOBAL, msg.PostPolicy, 0, info, msg.Timestamp.Unix(), payload)); err != nil {
									return false
								}
							}
						} else {
							if err := write(bmpPeerRoute(bmp.BMP_PEER_TYPE_GLOBAL, msg.PostPolicy, 0, info, msg.Timestamp.Unix(), msg.Payload)); err != nil {
								return false
							}
						}
					case *WatchEventPeerState:
						info := &table.PeerInfo{
							Address: msg.PeerAddress,
							AS:      msg.PeerAS,
							ID:      msg.PeerID,
						}
						if msg.State == bgp.BGP_FSM_ESTABLISHED {
							if err := write(bmpPeerUp(msg.LocalAddress.String(), msg.LocalPort, msg.PeerPort, msg.SentOpen, msg.RecvOpen, bmp.BMP_PEER_TYPE_GLOBAL, false, 0, info, msg.Timestamp.Unix())); err != nil {
								return false
							}
						} else {
							if err := write(bmpPeerDown(bmp.BMP_PEER_DOWN_REASON_UNKNOWN, bmp.BMP_PEER_TYPE_GLOBAL, false, 0, info, msg.Timestamp.Unix())); err != nil {
								return false
							}
						}
					}
				case <-b.dead:
					conn.Close()
					return true
				}
			}
		}() {
			return
		}
	}
}

type bmpClient struct {
	s      *BgpServer
	dead   chan struct{}
	host   string
	typ    config.BmpRouteMonitoringPolicyType
	ribout ribout
}

func bmpPeerUp(laddr string, lport, rport uint16, sent, recv *bgp.BGPMessage, t uint8, policy bool, pd uint64, peeri *table.PeerInfo, timestamp int64) *bmp.BMPMessage {
	ph := bmp.NewBMPPeerHeader(t, policy, pd, peeri.Address.String(), peeri.AS, peeri.ID.String(), float64(timestamp))
	return bmp.NewBMPPeerUpNotification(*ph, laddr, lport, rport, sent, recv)
}

func bmpPeerDown(reason uint8, t uint8, policy bool, pd uint64, peeri *table.PeerInfo, timestamp int64) *bmp.BMPMessage {
	ph := bmp.NewBMPPeerHeader(t, policy, pd, peeri.Address.String(), peeri.AS, peeri.ID.String(), float64(timestamp))
	return bmp.NewBMPPeerDownNotification(*ph, reason, nil, []byte{})
}

func bmpPeerRoute(t uint8, policy bool, pd uint64, peeri *table.PeerInfo, timestamp int64, payload []byte) *bmp.BMPMessage {
	ph := bmp.NewBMPPeerHeader(t, policy, pd, peeri.Address.String(), peeri.AS, peeri.ID.String(), float64(timestamp))
	m := bmp.NewBMPRouteMonitoring(*ph, nil)
	body := m.Body.(*bmp.BMPRouteMonitoring)
	body.BGPUpdatePayload = payload
	return m
}

func (b *bmpClientManager) addServer(c *config.BmpServerConfig) error {
	host := net.JoinHostPort(c.Address, strconv.Itoa(int(c.Port)))
	if _, y := b.clientMap[host]; y {
		return fmt.Errorf("bmp client %s is already configured", host)
	}
	b.clientMap[host] = &bmpClient{
		s:      b.s,
		dead:   make(chan struct{}),
		host:   host,
		typ:    c.RouteMonitoringPolicy,
		ribout: newribout(),
	}
	go b.clientMap[host].loop()
	return nil
}

func (b *bmpClientManager) deleteServer(c *config.BmpServerConfig) error {
	host := net.JoinHostPort(c.Address, strconv.Itoa(int(c.Port)))
	if c, y := b.clientMap[host]; !y {
		return fmt.Errorf("bmp client %s isn't found", host)
	} else {
		c.Stop()
		delete(b.clientMap, host)
	}
	return nil
}

type bmpClientManager struct {
	s         *BgpServer
	clientMap map[string]*bmpClient
}

func newBmpClientManager(s *BgpServer) *bmpClientManager {
	return &bmpClientManager{
		s:         s,
		clientMap: make(map[string]*bmpClient),
	}
}
