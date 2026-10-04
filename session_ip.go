package kvm

import (
	"net"
	"strings"
)

// sessionDisplayLabel is shown at the top right of the device display: the
// address of the browser using the console instead of the session count.
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
