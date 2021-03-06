package ogo

import (
	"github.com/jonstout/ogo/protocol/eth"
	"github.com/jonstout/ogo/protocol/ofp10"
	"github.com/jonstout/ogo/protocol/util"

	"log"
	"net"
	"time"
)

// OgoInstance generator.
func NewInstance() interface{} {
	return new(OgoInstance)
}

type OgoInstance struct {
	shutdown chan bool
}

func (o *OgoInstance) ConnectionUp(dpid net.HardwareAddr) {
	dropMod := ofp10.NewFlowMod()
	dropMod.Priority = 1

	arpFmod := ofp10.NewFlowMod()
	arpFmod.Priority = 2
	arpFmod.Match.DLType = 0x0806 // ARP Messages
	arpFmod.AddAction(ofp10.NewActionOutput(ofp10.P_CONTROLLER))

	dscFmod := ofp10.NewFlowMod()
	dscFmod.Priority = 0xffff
	dscFmod.Match.DLType = 0xa0f1 // Link Discovery Messages
	dscFmod.AddAction(ofp10.NewActionOutput(ofp10.P_CONTROLLER))

	if sw, ok := Switch(dpid); ok {
		sw.Send(ofp10.NewFeaturesRequest())
		sw.Send(dropMod)
		sw.Send(arpFmod)
		sw.Send(dscFmod)
		sw.Send(ofp10.NewEchoRequest())
	}
	go o.linkDiscoveryLoop(dpid)
}

func (o *OgoInstance) ConnectionDown(dpid net.HardwareAddr) {
	o.shutdown <- true
	log.Println("Switch Disconnected:", dpid)
}

func (o *OgoInstance) EchoRequest(dpid net.HardwareAddr) {
	// Wait three seconds then send an echo_reply message.
	go func() {
		<-time.After(time.Second * 3)
		if sw, ok := Switch(dpid); ok {
			res := ofp10.NewEchoReply()
			sw.Send(res)
		}
	}()
}

func (o *OgoInstance) EchoReply(dpid net.HardwareAddr) {
	// Wait three seconds then send an echo_request message.
	go func() {
		<-time.After(time.Second * 3)
		if sw, ok := Switch(dpid); ok {
			res := ofp10.NewEchoRequest()
			sw.Send(res)
		}
	}()
}

func (o *OgoInstance) FeaturesReply(dpid net.HardwareAddr, features *ofp10.SwitchFeatures) {
	if sw, ok := Switch(dpid); ok {
		for _, p := range features.Ports {
			sw.SetPort(p.PortNo, p)
		}
	}
}

func (o *OgoInstance) PacketIn(dpid net.HardwareAddr, msg *ofp10.PacketIn) {
	eth := msg.Data
	if buf, ok := eth.Data.(*util.Buffer); ok && eth.Ethertype == 0xa0f1 {
		linkMsg := NewLinkDiscovery()
		if err := linkMsg.UnmarshalBinary(buf.Bytes()); err != nil {
			log.Println(err)
			return
		}

		latency := time.Since(time.Unix(0, linkMsg.Nsec))
		l := &Link{linkMsg.SrcDPID, msg.InPort, latency, -1}

		if sw, ok := Switch(dpid); ok {
			sw.setLink(dpid, l)
		}
	}
}

func (o *OgoInstance) linkDiscoveryLoop(dpid net.HardwareAddr) {
	for {
		select {
		case <-o.shutdown:
			return
		// Every two seconds send a link discovery packet.
		case <-time.After(time.Second * 2):
			e := eth.New()
			e.Ethertype = 0xa0f1
			e.HWSrc = dpid[2:]
			linkDsc := NewLinkDiscovery()
			linkDsc.SrcDPID = dpid
			e.Data = linkDsc

			pkt := ofp10.NewPacketOut()
			pkt.Data = e
			pkt.AddAction(ofp10.NewActionOutput(ofp10.P_ALL))

			if sw, ok := Switch(dpid); ok {
				sw.Send(pkt)
			}
		}
	}
}
