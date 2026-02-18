package rpc

import (
	"encoding/json"

	"github.com/eth2028/eth2028/core/types"
)

// SubType distinguishes the kind of WebSocket subscription.
type SubType int

const (
	// SubNewHeads watches for new block headers.
	SubNewHeads SubType = iota
	// SubLogs watches for matching log events.
	SubLogs
	// SubPendingTx watches for new pending transaction hashes.
	SubPendingTx
)

// Subscription represents an active WebSocket subscription.
type Subscription struct {
	ID    string
	Type  SubType
	Query FilterQuery
	ch    chan interface{}
}

// Channel returns the notification channel for this subscription.
func (s *Subscription) Channel() <-chan interface{} {
	return s.ch
}

// subscriptionBufferSize is the channel buffer for subscription notifications.
const subscriptionBufferSize = 128

// Subscribe creates a new WebSocket subscription and returns its ID.
// The SubscriptionManager tracks subscriptions separately from poll-based filters.
func (sm *SubscriptionManager) Subscribe(subType SubType, query FilterQuery) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := sm.generateID()
	sub := &Subscription{
		ID:    id,
		Type:  subType,
		Query: query,
		ch:    make(chan interface{}, subscriptionBufferSize),
	}
	sm.subscriptions[id] = sub
	return id
}

// Unsubscribe removes a subscription by ID. Returns true if it existed.
func (sm *SubscriptionManager) Unsubscribe(id string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sub, ok := sm.subscriptions[id]
	if ok {
		close(sub.ch)
		delete(sm.subscriptions, id)
	}
	return ok
}

// GetSubscription returns a subscription by ID, or nil if not found.
func (sm *SubscriptionManager) GetSubscription(id string) *Subscription {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.subscriptions[id]
}

// SubscriptionCount returns the number of active subscriptions.
func (sm *SubscriptionManager) SubscriptionCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.subscriptions)
}

// NotifyNewHead broadcasts a new block header to all "newHeads" subscribers.
func (sm *SubscriptionManager) NotifyNewHead(header *types.Header) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	formatted := FormatHeader(header)
	for _, sub := range sm.subscriptions {
		if sub.Type == SubNewHeads {
			select {
			case sub.ch <- formatted:
			default:
				// Drop if buffer is full; subscriber is too slow.
			}
		}
	}
}

// NotifyLogs broadcasts matching logs to "logs" subscribers.
func (sm *SubscriptionManager) NotifyLogs(logs []*types.Log) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, sub := range sm.subscriptions {
		if sub.Type != SubLogs {
			continue
		}
		for _, log := range logs {
			if MatchFilter(log, sub.Query) {
				formatted := FormatLog(log)
				select {
				case sub.ch <- formatted:
				default:
					// Drop if buffer is full.
				}
			}
		}
	}
}

// NotifyPendingTx broadcasts a pending transaction hash to all
// "newPendingTransactions" subscribers.
func (sm *SubscriptionManager) NotifyPendingTxHash(txHash types.Hash) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	hashStr := encodeHash(txHash)
	for _, sub := range sm.subscriptions {
		if sub.Type == SubPendingTx {
			select {
			case sub.ch <- hashStr:
			default:
				// Drop if buffer is full.
			}
		}
	}
}

// WSNotification is a JSON-RPC 2.0 subscription notification sent over WebSocket.
type WSNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// WSSubscriptionResult wraps the subscription ID and result for notifications.
type WSSubscriptionResult struct {
	Subscription string      `json:"subscription"`
	Result       interface{} `json:"result"`
}

// FormatWSNotification creates a JSON-RPC subscription notification message.
func FormatWSNotification(subID string, result interface{}) *WSNotification {
	params := WSSubscriptionResult{
		Subscription: subID,
		Result:       result,
	}
	raw, _ := json.Marshal(params)
	return &WSNotification{
		JSONRPC: "2.0",
		Method:  "eth_subscription",
		Params:  raw,
	}
}

