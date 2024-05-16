package sip

import (
	"bytes"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/fakes"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

func TestClientTransactionInviteFSM(t *testing.T) {
	// make things fast
	SetTimers(1*time.Millisecond, 1*time.Millisecond, 1*time.Millisecond)
	req, _, _ := testCreateInvite(t, "127.0.0.99:5060", "udp", "127.0.0.2:5060")

	incoming := bytes.NewBuffer([]byte{})
	outgoing := bytes.NewBuffer([]byte{})
	conn := &UDPConnection{
		PacketConn: &fakes.UDPConn{
			Reader:  incoming,
			Writers: map[string]io.Writer{"127.0.0.99:5060": outgoing},
		},
	}
	tx := NewClientTx("123", req, conn, log.Logger)

	// CALLING STATE
	err := tx.Init()
	require.NoError(t, err)
	require.NoError(t, compareFunctions(tx.currentFsmState(), tx.inviteStateCalling))

	// PROCEEDING STATE
	res100 := NewResponseFromRequest(req, StatusTrying, "Trying", nil)

	go func() { <-tx.Responses() }() // this will now block our transaction until termination or 200 is consumed
	tx.receive(res100)
	require.NoError(t, compareFunctions(tx.currentFsmState(), tx.inviteStateProcceeding))

	// ACCEPTING STATE RFC 6026 or not allowing 2xx to kill transaction because it
	// in case of proxy more 2xx still may need to be retransmitted as part of transaction and avoid dying imediately
	// make timer M small

	res200 := NewResponseFromRequest(req, StatusOK, "OK", nil)
	go func() { <-tx.Responses() }()
	tx.receive(res200)
	require.NoError(t, compareFunctions(tx.currentFsmState(), tx.inviteStateAccepted))

	// COMPLETED STATE
	time.Sleep(Timer_M * 2)
	require.NoError(t, compareFunctions(tx.currentFsmState(), tx.inviteStateTerminated))

	// res200 := NewResponseFromRequest(req, StatusOK, "OK", nil)
	// incoming.WriteString(res200.String())

}

func TestClientTransactionResponseRace(t *testing.T) {
	// make things fast
	SetTimers(1*time.Millisecond, 1*time.Millisecond, 1*time.Millisecond)
	req, _, _ := testCreateInvite(t, "127.0.0.99:5060", "udp", "127.0.0.2:5060")

	incoming := bytes.NewBuffer([]byte{})
	outgoing := bytes.NewBuffer([]byte{})
	conn := &UDPConnection{
		PacketConn: &fakes.UDPConn{
			Reader:  incoming,
			Writers: map[string]io.Writer{"127.0.0.99:5060": outgoing},
		},
	}
	tx := NewClientTx("123", req, conn, log.Logger)

	// CALLING STATE
	err := tx.Init()
	require.NoError(t, err)
	require.NoError(t, compareFunctions(tx.currentFsmState(), tx.inviteStateCalling))

	max := 10

	resInput := make([]*Response, max, max)
	resOutput := make([]*Response, 0, max)

	for i := 0; i < max; i++ {
		resInput[i] = NewResponseFromRequest(req, StatusTrying, "Trying", []byte(fmt.Sprintf("%d", i)))
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := 0; i < max; i++ {
			r := <-tx.Responses()
			resOutput = append(resOutput, r)
		}
		wg.Done()
	}()
	for i := 0; i < max; i++ {
		wg.Add(1)
		go func(n int) {
			tx.receive(resInput[n])
			wg.Done()
		}(i)
	}
	wg.Wait()
	require.Equal(t, max, len(resOutput))
	ok := true
	for i := 0; i < max; i++ {
		in := string(resInput[i].Body())
		count := 0
		for j := 0; j < max; j++ {
			if string(resOutput[j].Body()) == in {
				count++
			}
		}
		if count != 1 {
			t.Logf("msg %s is found %d times\n", in, count)
			ok = false
		}
	}
	require.True(t, ok)

}
