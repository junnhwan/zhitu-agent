package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"

	"github.com/zhitu-agent/zhitu-agent/internal/config"
)

const (
	memoryKeyPrefix = "chat:memory:"
	lockKeyPrefix   = "chat:memory:lock:"
)

// CompressibleMemory implements Redis-backed chat memory with compression.
// Mirrors Java CompressibleChatMemory — distributed lock + compression + atomic update.
type CompressibleMemory struct {
	sessionID            int64
	client               *redis.Client
	compressor           Compressor
	microCompactor       *MicroCompactor
	lock                 *DistributedLock
	maxMessages          int
	tokenThreshold       int
	fallbackRecentRounds int
	ttlSeconds           int
}

func NewCompressibleMemory(sessionID int64, client *redis.Client, cfg *config.ChatMemoryConfig, compressor Compressor, microCompactor *MicroCompactor) *CompressibleMemory {
	return &CompressibleMemory{
		sessionID:            sessionID,
		client:               client,
		compressor:           compressor,
		microCompactor:       microCompactor,
		lock:                 NewDistributedLock(client, cfg.Redis.Lock.ExpireSeconds, cfg.Redis.Lock.RetryTimes, cfg.Redis.Lock.RetryIntervalMs),
		maxMessages:          cfg.MaxMessages,
		tokenThreshold:       cfg.Compression.TokenThreshold,
		fallbackRecentRounds: cfg.Compression.FallbackRecentRounds,
		ttlSeconds:           cfg.Redis.TTLSeconds,
	}
}

// Add adds a message to the chat memory with lock + compression logic.
// Mirrors Java CompressibleChatMemory.add().
func (m *CompressibleMemory) Add(ctx context.Context, message *schema.Message) {
	if m.microCompactor != nil {
		message = m.microCompactor.MessageForMemory(ctx, message)
	}

	lockKey := fmt.Sprintf("%s%d", lockKeyPrefix, m.sessionID)
	lockValue, acquired := m.lock.Acquire(ctx, lockKey)

	if !acquired {
		log.Printf("[Memory] session %d lock acquisition failed, degrading to simple append", m.sessionID)
		messages := m.GetMessages(ctx)
		messages = append(messages, message)
		m.updateMessages(ctx, messages)
		return
	}
	defer m.lock.Release(ctx, lockKey, lockValue)

	messages := m.GetMessages(ctx)
	messages = append(messages, message)

	// Check if compression is needed
	needCompress := false
	currentTokenCount := 0
	if m.compressor != nil {
		currentTokenCount = m.compressor.EstimateTokens(messages)
		needCompress = len(messages) > m.maxMessages || currentTokenCount > m.tokenThreshold
	} else {
		needCompress = len(messages) > m.maxMessages
	}

	if needCompress {
		log.Printf("[Memory] session %d triggering compression — msgs: %d/%d, tokens: %d/%d",
			m.sessionID, len(messages), m.maxMessages, currentTokenCount, m.tokenThreshold)

		compressed, err := m.tryCompress(ctx, messages)
		if err != nil {
			log.Printf("[Memory] session %d compression failed, falling back to recent %d rounds: %v",
				m.sessionID, m.fallbackRecentRounds, err)
			compressed = m.fallbackToRecent(messages)
		}
		m.atomicUpdateMessages(ctx, compressed)
	} else {
		m.updateMessages(ctx, messages)
	}
}

// GetMessages retrieves all messages for this session from Redis.
// Mirrors Java CompressibleChatMemory.messages().
func (m *CompressibleMemory) GetMessages(ctx context.Context) []*schema.Message {
	key := fmt.Sprintf("%s%d", memoryKeyPrefix, m.sessionID)
	data, err := m.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		log.Printf("[Memory] session %d get messages failed: %v", m.sessionID, err)
		return nil
	}

	var messages []*schema.Message
	if err := json.Unmarshal(data, &messages); err != nil {
		log.Printf("[Memory] session %d unmarshal messages failed: %v", m.sessionID, err)
		return nil
	}

	return messages
}

// Clear deletes all messages for this session.
// Mirrors Java CompressibleChatMemory.clear().
func (m *CompressibleMemory) Clear(ctx context.Context) {
	lockKey := fmt.Sprintf("%s%d", lockKeyPrefix, m.sessionID)
	lockValue, acquired := m.lock.Acquire(ctx, lockKey)
	if acquired {
		defer m.lock.Release(ctx, lockKey, lockValue)
	}

	key := fmt.Sprintf("%s%d", memoryKeyPrefix, m.sessionID)
	m.client.Del(ctx, key)
}

// tryCompress attempts to compress messages using the compressor.
func (m *CompressibleMemory) tryCompress(ctx context.Context, messages []*schema.Message) ([]*schema.Message, error) {
	if m.compressor == nil {
		return messages, nil
	}
	compressed := m.compressor.Compress(ctx, messages)
	return compressed, nil
}

// fallbackToRecent keeps only the most recent N rounds when compression fails.
// Mirrors Java fallback: messages.subList(size - fallbackRecentRounds, size)
func (m *CompressibleMemory) fallbackToRecent(messages []*schema.Message) []*schema.Message {
	if len(messages) > m.fallbackRecentRounds {
		return messages[len(messages)-m.fallbackRecentRounds:]
	}
	return messages
}

// updateMessages writes messages to Redis with TTL.
func (m *CompressibleMemory) updateMessages(ctx context.Context, messages []*schema.Message) {
	key := fmt.Sprintf("%s%d", memoryKeyPrefix, m.sessionID)
	data, err := json.Marshal(messages)
	if err != nil {
		log.Printf("[Memory] session %d marshal messages failed: %v", m.sessionID, err)
		return
	}
	m.client.Set(ctx, key, data, time.Duration(m.ttlSeconds)*time.Second)
}

// atomicUpdateMessages uses Redis transaction (MULTI/EXEC) to atomically update messages.
// Mirrors Java atomicUpdateMessages — delete old then write new in a transaction.
func (m *CompressibleMemory) atomicUpdateMessages(ctx context.Context, messages []*schema.Message) {
	key := fmt.Sprintf("%s%d", memoryKeyPrefix, m.sessionID)
	data, err := json.Marshal(messages)
	if err != nil {
		log.Printf("[Memory] session %d marshal messages failed: %v", m.sessionID, err)
		m.updateMessages(ctx, messages)
		return
	}

	_, err = m.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, key)
		pipe.Set(ctx, key, data, time.Duration(m.ttlSeconds)*time.Second)
		return nil
	})

	if err != nil {
		log.Printf("[Memory] session %d atomic update failed, falling back to simple update: %v", m.sessionID, err)
		m.updateMessages(ctx, messages)
	}
}
