package dockercli

import "testing"

// "Session open refused by peer" is the MaxSessions signature. It usually
// arrives alongside "Connection closed by", but not always — when it appears
// alone it must still be recognised as an SSH transport failure so the retry
// path treats it as one.
func TestIsSSHConnectionError_SessionOpenRefused(t *testing.T) {
	if !isSSHConnectionError("mux_client_request_session: session request failed: Session open refused by peer") {
		t.Error("Session open refused by peer must be recognised as an SSH connection error")
	}
}

func TestIsSSHConnectionError_UnrelatedStderrIsNot(t *testing.T) {
	if isSSHConnectionError("Error response from daemon: no such image: foo:latest") {
		t.Error("an unrelated docker error must not be treated as an SSH connection error")
	}
}
