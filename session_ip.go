package kvm

import (
	"net"
	"strings"
)

// sessionDisplayLabel is shown on the device display where the device IPv4
// address was (the device address moves to the session count's place): the
// address of the browser using the console.
func sessionDisplayLabel() string {
	session := currentSession
	if session == nil || getActiveSessions() == 0 {
		return "No session"
	}
	if session.clientIP == "" {
		return "Cloud"
	}
	return session.clientIP
}

// setClientIPFromICE takes the browser address from the selected ICE
// candidate pair when signaling did not give one (cloud sessions). mDNS
// names and relay addresses do not identify the browser and are skipped.
func (s *Session) setClientIPFromICE() {
	if s.clientIP != "" || s.peerConnection == nil {
		return
	}
	sctp := s.peerConnection.SCTP()
	if sctp == nil || sctp.Transport() == nil || sctp.Transport().ICETransport() == nil {
		return
	}
	pair, err := sctp.Transport().ICETransport().GetSelectedCandidatePair()
	if err != nil || pair == nil || pair.Remote == nil {
		return
	}
	if pair.Remote.Typ.String() == "relay" || strings.HasSuffix(pair.Remote.Address, ".local") {
		return
	}
	if ip := net.ParseIP(pair.Remote.Address); ip != nil {
		s.clientIP = ip.String()
	}
}
