package imapclient_test

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestByePreservesResponse(t *testing.T) {
	for _, greeting := range []bool{false, true} {
		t.Run(fmt.Sprintf("greeting=%v", greeting), func(t *testing.T) {
			conn, server := net.Pipe()
			server.SetDeadline(time.Now().Add(5 * time.Second))
			client := imapclient.New(conn, nil)
			defer client.Close()
			defer server.Close()
			done := make(chan error, 1)
			go func() {
				if !greeting {
					if _, err := fmt.Fprint(server, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
						done <- err
						return
					}
					if _, err := bufio.NewReader(server).ReadString('\n'); err != nil {
						done <- err
						return
					}
				}
				_, err := fmt.Fprint(server, "* BYE [OVERQUOTA] Account exceeded command or bandwidth limits.\r\n")
				done <- err
			}()
			var err error
			if greeting {
				err = client.WaitGreeting()
			} else {
				err = client.Noop().Wait()
			}
			var response *imap.Error
			if !errors.As(err, &response) || response.Type != imap.StatusResponseTypeBye || response.Code != imap.ResponseCodeOverQuota || response.Text != "Account exceeded command or bandwidth limits." {
				t.Fatalf("lost BYE response: %v", err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			err = client.Close()
			if !errors.As(err, &response) || response.Code != imap.ResponseCodeOverQuota {
				t.Fatalf("Close lost BYE: %v", err)
			}
		})
	}
}
