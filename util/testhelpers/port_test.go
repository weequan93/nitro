package testhelpers

import (
	"net"
	"testing"
)

func TestFreeTCPPortListener(t *testing.T) {
	aListener, err := FreeTCPPortListener()
	if err != nil {
		t.Fatal(err)
	}
	bListener, err := FreeTCPPortListener()
	if err != nil {
		t.Fatal(err)
	}

	if aListenerAddr, ok := aListener.Addr().(*net.TCPAddr); ok {
		if bListenerAddr, ok := bListener.Addr().(*net.TCPAddr); ok {
			if aListenerAddr.Port == bListenerAddr.Port {
				t.Errorf("FreeTCPPortListener() got same port: %v, %v", aListener, bListener)
			}
		}
	}

	if aListenerAddr, ok := aListener.Addr().(*net.TCPAddr); ok {
		if bListenerAddr, ok := bListener.Addr().(*net.TCPAddr); ok {
			if aListenerAddr.Port == 0 || bListenerAddr.Port == 0 {
				t.Errorf("FreeTCPPortListener() got port 0")
			}
		}
	}
}
