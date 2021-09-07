package itest

import (
	"context"
	"github.com/btcsuite/btcd/wire"
	"time"

	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/chainreg"
	"github.com/lightningnetwork/lnd/lnrpc"
	"github.com/lightningnetwork/lnd/lnrpc/routerrpc"
	"github.com/lightningnetwork/lnd/lntest"
)

func testSingleHopInvoice(net *lntest.NetworkHarness, t *harnessTest) {
	ctxb := context.Background()

	// Create new node for Carol
	Carol := net.NewNode(t.t, "Carol", nil)
	defer shutdownAndAssert(net, t, Carol)

	net.ConnectNodes(t.t, net.Bob, Carol)

	// Open a channel with 100k satoshi between Alice and Bob with Alice being
	// the sole funder of the channel.
	chanAmt := btcutil.Amount(100000)
	chanPointAliceBob := openChannelAndAssert(
		t, net, net.Alice, net.Bob,
		lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	aliceChanTXID, err := lnrpc.GetChanPointFundingTxid(chanPointAliceBob)
	if err != nil {
		t.Fatalf("unable to get txid: %v", err)
	}
	aliceFundPoint := wire.OutPoint{
		Hash:  *aliceChanTXID,
		Index: chanPointAliceBob.OutputIndex,
	}

	// Open a channel with 100k satoshi between Bob and Carol with Bob being
	// the sole funder of the channel.
	chanPointBobCarol := openChannelAndAssert(
		t, net, net.Bob, Carol,
		lntest.OpenChannelParams{
			Amt: chanAmt,
		},
	)
	bobChanTXID, err := lnrpc.GetChanPointFundingTxid(chanPointBobCarol)
	if err != nil {
		t.Fatalf("unable to get txid: %v", err)
	}
	bobFundPoint := wire.OutPoint{
		Hash:  *bobChanTXID,
		Index: chanPointBobCarol.OutputIndex,
	}

	// Wait for channel open gossip to spread
	ctxt, _ := context.WithTimeout(ctxb, defaultTimeout)
	err = net.Alice.WaitForNetworkChannelOpen(ctxt, chanPointAliceBob)
	if err != nil {
		t.Fatalf("alice didn't advertise channel before "+
			"timeout: %v", err)
	}
	err = net.Bob.WaitForNetworkChannelOpen(ctxt, chanPointAliceBob)
	if err != nil {
		t.Fatalf("bob didn't advertise channel before "+
			"timeout: %v", err)
	}
	err = net.Bob.WaitForNetworkChannelOpen(ctxt, chanPointBobCarol)
	if err != nil {
		t.Fatalf("bob didn't advertise channel before "+
			"timeout: %v", err)
	}
	err = Carol.WaitForNetworkChannelOpen(ctxt, chanPointBobCarol)
	if err != nil {
		t.Fatalf("carol didn't advertise channel before "+
			"timeout: %v", err)
	}

	time.Sleep(time.Millisecond * 50)

	// Carol creates payment request for 1000 sats
	const numPayments = 1
	const paymentAmt = 100
	payReqs, _, _, err := createPayReqs(
		Carol, paymentAmt, numPayments,
	)
	if err != nil {
		t.Fatalf("unable to create pay reqs: %v", err)
	}

	// subscribe to htlc events
	ctxt, cancel := context.WithTimeout(ctxb, defaultTimeout)
	defer cancel()
	aliceEvents, err := net.Alice.RouterClient.SubscribeHtlcEvents(
		ctxt, &routerrpc.SubscribeHtlcEventsRequest{},
	)
	if err != nil {
		t.Fatalf("could not subscribe events: %v", err)
	}
	bobEvents, err := net.Bob.RouterClient.SubscribeHtlcEvents(
		ctxt, &routerrpc.SubscribeHtlcEventsRequest{},
	)
	if err != nil {
		t.Fatalf("could not subscribe events: %v", err)
	}
	carolEvents, err := Carol.RouterClient.SubscribeHtlcEvents(
		ctxt, &routerrpc.SubscribeHtlcEventsRequest{},
	)
	if err != nil {
		t.Fatalf("could not subscribe events: %v", err)
	}

	// Alice pays Carol's invoice
	err = completePaymentRequests(net.Alice, net.Alice.RouterClient, payReqs, true)
	if err != nil {
		t.Fatalf("unable to send payments: %v", err)
	}

	// Check each node's amount paid in the HTLC

	assertAmountPaid(t, "Bob(local) => Carol(remote)", net.Bob,
		bobFundPoint, int64(paymentAmt), int64(0))
	assertAmountPaid(t, "Bob(local) => Carol(remote)", Carol,
		bobFundPoint, int64(0), int64(paymentAmt))
	assertAmountPaid(t, "Alice(local) => Bob(remote)", net.Alice,
		aliceFundPoint, expectedAmountPaid, int64(0))
	assertAmountPaid(t, "Alice(local) => Bob(remote)", net.Bob,
		aliceFundPoint, int64(0), int64(paymentAmt))

	// Alice should have SEND event
	assertHtlcEvents(
		t, numPayments, 0, numPayments, routerrpc.HtlcEvent_SEND,
		aliceEvents,
	)

	// Bob should have FORWARD event 
	assertHtlcEvents(
		t, numPayments, 0, numPayments, routerrpc.HtlcEvent_FORWARD,
		bobEvents,
	)

	// Carol should have RECEIVE event
	assertHtlcEvents(
		t, 0, 0, numPayments, routerrpc.HtlcEvent_RECEIVE,
		carolEvents,
	)
	
	// end test
	closeChannelAndAssert(t, net, net.Alice, chanPointAliceBob, false)
	closeChannelAndAssert(t, net, net.Bob, chanPointBobCarol, false)

}
