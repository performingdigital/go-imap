package imapclient_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestFetchMessageBodyStructure(t *testing.T) {
	const envelope = `(NIL NIL NIL NIL NIL NIL NIL NIL NIL NIL)`
	const body = `("TEXT" "PLAIN" NIL NIL NIL "7BIT" 10 1)`
	const disposition = `("ATTACHMENT" ("FILENAME" "forwarded.eml")) NIL`
	for _, test := range []struct {
		name, subtype, suffix      string
		extended, message, invalid bool
	}{
		{name: "Gmail omitted fields", subtype: "RFC822", suffix: " NIL " + disposition, extended: true},
		{name: "omitted fields with MD5", subtype: "RFC822", suffix: ` "checksum" ` + disposition, extended: true},
		{name: "omitted fields with literal MD5", subtype: "RFC822", suffix: " {8}\r\nchecksum " + disposition, extended: true},
		{name: "omitted fields without extensions", subtype: "RFC822"},
		{name: "regular RFC822", subtype: "RFC822", suffix: " " + envelope + " " + body + " 1 NIL " + disposition, extended: true, message: true},
		{name: "regular RFC822 without extensions", subtype: "RFC822", suffix: " " + envelope + " " + body + " 1", message: true},
		{name: "regular global", subtype: "global", suffix: " " + envelope + " " + body + " 1 NIL " + disposition, extended: true, message: true},
		{name: "omitted global fields", subtype: "global", suffix: " NIL " + disposition, extended: true},
		{name: "malformed envelope still rejected", subtype: "RFC822", suffix: " (NIL) " + body + " 1", invalid: true},
		{name: "missing line count still rejected", subtype: "RFC822", suffix: " " + envelope + " " + body + " NIL", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			local, remote := net.Pipe()
			defer local.Close()
			defer remote.Close()
			local.SetDeadline(time.Now().Add(5 * time.Second))
			remote.SetDeadline(time.Now().Add(5 * time.Second))
			done := make(chan error, 1)
			go func() {
				defer remote.Close()
				fmt.Fprint(remote, "* PREAUTH [CAPABILITY IMAP4rev1] ready\r\n")
				reader := bufio.NewReader(remote)
				line, err := reader.ReadString('\n')
				if err != nil {
					done <- err
					return
				}
				if !strings.Contains(line, "UID FETCH 948") {
					done <- fmt.Errorf("unexpected command: %s", line)
					return
				}
				tag := strings.Fields(line)[0]
				fmt.Fprintf(remote, "* 1 FETCH (UID 948 BODYSTRUCTURE (%s(\"MESSAGE\" %q (\"NAME\" \"forwarded.eml\") NIL NIL \"8BIT\" 1810%s) \"MIXED\"))\r\n%s OK fetched\r\n", body, test.subtype, test.suffix, tag)
				if !test.invalid {
					line, err = reader.ReadString('\n')
					if err != nil {
						done <- err
						return
					}
					if !strings.Contains(line, "NOOP") {
						done <- fmt.Errorf("expected NOOP: %s", line)
						return
					}
					fmt.Fprintf(remote, "%s OK alive\r\n", strings.Fields(line)[0])
				}
				done <- nil
			}()
			client := imapclient.New(local, nil)
			defer client.Close()
			messages, err := client.Fetch(imap.UIDSetNum(948), &imap.FetchOptions{UID: true, BodyStructure: &imap.FetchItemBodyStructure{Extended: true}}).Collect()
			if test.invalid {
				if err == nil {
					t.Fatal("malformed message accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 1 || messages[0].UID != 948 {
				t.Fatal("missing message")
			}
			root := messages[0].BodyStructure.(*imap.BodyStructureMultiPart)
			if len(root.Children) != 2 {
				t.Fatal("lost sibling part")
			}
			part := root.Children[1].(*imap.BodyStructureSinglePart)
			if part.Type != "MESSAGE" || part.Subtype != test.subtype || part.Size != 1810 || part.Params["name"] != "forwarded.eml" {
				t.Fatalf("lost MIME metadata: %+v", part)
			}
			if (part.MessageRFC822 != nil) != test.message {
				t.Fatal("unexpected encapsulated message structure")
			}
			if test.message && (part.MessageRFC822.Envelope == nil || part.MessageRFC822.BodyStructure == nil || part.MessageRFC822.NumLines != 1) {
				t.Fatal("lost encapsulated message fields")
			}
			if (part.Extended != nil) != test.extended {
				t.Fatal("unexpected extension fields")
			}
			if test.extended && (part.Extended.Disposition == nil || part.Extended.Disposition.Value != "ATTACHMENT" || part.Extended.Disposition.Params["filename"] != "forwarded.eml") {
				t.Fatal("lost attachment disposition")
			}
			if err := client.Noop().Wait(); err != nil {
				t.Fatal(err)
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
