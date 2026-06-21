package app_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sidus.io/charge/internal/app"
)

func TestSwitchBoard_RegisterAndSendMessage(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	signals := make(chan app.Signal)
	id := "conn-1"

	board.Register(id, signals)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sig := <-signals
		assert.Equal(t, []byte("hello"), sig.Message)
		sig.Result <- nil
	}()

	ctx := context.Background()
	err := board.SendMessage(ctx, id, []byte("hello"))
	assert.NoError(t, err)
	<-done
}

func TestSwitchBoard_SendMessage_ConnectionNotFound(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()

	ctx := context.Background()
	err := board.SendMessage(ctx, "unknown", []byte("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, app.ErrConnectionNotFound)
}

func TestSwitchBoard_RegisterUnregisterSend(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	signals := make(chan app.Signal)
	id := "conn-1"

	board.Register(id, signals)
	board.Unregister(id)

	ctx := context.Background()
	err := board.SendMessage(ctx, id, []byte("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, app.ErrConnectionNotFound)
}

func TestSwitchBoard_RegisterReplacesExisting(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	id := "conn-1"

	oldSignals := make(chan app.Signal)
	newSignals := make(chan app.Signal)

	board.Register(id, oldSignals)
	board.Register(id, newSignals)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sig := <-newSignals
		assert.Equal(t, []byte("hello"), sig.Message)
		sig.Result <- nil
	}()

	ctx := context.Background()
	err := board.SendMessage(ctx, id, []byte("hello"))
	assert.NoError(t, err)
	<-done
}

func TestSwitchBoard_UnregisterMissingConnection(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()

	assert.NotPanics(t, func() {
		board.Unregister("never-registered")
	})
}

func TestSwitchBoard_SendMessage_ConsumerReturnsError(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	signals := make(chan app.Signal)
	id := "conn-1"

	board.Register(id, signals)

	consumerErr := errors.New("consumer failed")
	go func() {
		sig := <-signals
		sig.Result <- consumerErr
	}()

	ctx := context.Background()
	err := board.SendMessage(ctx, id, []byte("test"))
	assert.ErrorIs(t, err, consumerErr)
}

func TestSwitchBoard_SendMessage_ContextAlreadyCancelled(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	signals := make(chan app.Signal)
	id := "conn-1"

	board.Register(id, signals)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := board.SendMessage(ctx, id, []byte("test"))
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestSwitchBoard_SendMessage_ContextCancelledWhileBlockedOnSend(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		board := app.NewSwitchBoard()
		signals := make(chan app.Signal)
		id := "conn-1"

		board.Register(id, signals)

		// Dont setup a receiver

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		go func() {
			time.Sleep(time.Hour)
			cancel()
		}()

		err := board.SendMessage(ctx, id, []byte("test"))
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSwitchBoard_SendMessage_ContextCancelledWaitingForAck(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {

		board := app.NewSwitchBoard()
		signals := make(chan app.Signal)
		id := "conn-1"

		board.Register(id, signals)

		go func() {
			<-signals
			// No one acks...
		}()

		ctx, cancel := context.WithCancel(context.Background())

		go func() {
			time.Sleep(time.Hour)
			cancel()
		}()

		err := board.SendMessage(ctx, id, []byte("test"))
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestSwitchBoard_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	board := app.NewSwitchBoard()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var consumerWg sync.WaitGroup

	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("conn-%d", i)
		signals := make(chan app.Signal)
		board.Register(id, signals)

		consumerWg.Go(
			func() {
				for {
					select {
					case <-ctx.Done():
						return
					case sig, ok := <-signals:
						if !ok {
							return
						}
						sig.Result <- nil
					}
				}
			},
		)
	}

	var sendWg sync.WaitGroup
	for i := 0; i < 100; i++ {
		sendWg.Go(
			func() {
				id := fmt.Sprintf("conn-%d", i%10)
				board.SendMessage(context.Background(), id, []byte("ping"))
			},
		)
	}

	sendWg.Wait()
	cancel()
	consumerWg.Wait()
}
