package redis

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
)

type stringCmdReply struct {
	val string
	err error
}

type scriptedRedisHook struct {
	hgetReplies []stringCmdReply
	getReplies  []stringCmdReply
}

func (h *scriptedRedisHook) DialHook(next redis.DialHook) redis.DialHook {
	return next
}

func (h *scriptedRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		switch strings.ToLower(cmd.Name()) {
		case "hget":
			if len(h.hgetReplies) == 0 {
				return errors.New("unexpected HGET")
			}
			reply := h.hgetReplies[0]
			h.hgetReplies = h.hgetReplies[1:]
			if err := setStringCmdVal(cmd, reply.val); err != nil {
				return err
			}
			return reply.err
		case "get":
			if len(h.getReplies) == 0 {
				return errors.New("unexpected GET")
			}
			reply := h.getReplies[0]
			h.getReplies = h.getReplies[1:]
			if err := setStringCmdVal(cmd, reply.val); err != nil {
				return err
			}
			return reply.err
		default:
			return errors.New("unexpected command: " + cmd.Name())
		}
	}
}

func (h *scriptedRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func setStringCmdVal(cmd redis.Cmder, val string) error {
	stringCmd, ok := cmd.(*redis.StringCmd)
	if !ok {
		return errors.New("unexpected cmd type")
	}
	stringCmd.SetVal(val)
	return nil
}

// newConnectedClientForTest 创建一个用于测试的 Redis 客户端
// 使用 hook 拦截所有命令，不需要实际的 Redis 连接
// DisableIndentity: true 防止初始化时尝试连接
func newConnectedClientForTest(t *testing.T, hook redis.Hook) *Client {
	t.Helper()

	redisClient := redis.NewClient(&redis.Options{
		Addr:            "127.0.0.1:6379", // 不会实际连接，hook 会拦截所有命令
		DisableIndentity: true,            // 禁用 CLIENT SETINFO，避免初始连接尝试
	})
	redisClient.AddHook(hook)

	return &Client{
		client:      redisClient,
		isConnected: true,
	}
}

func TestGetDailyCost_LegacyGetRedisNilReturnsZero(t *testing.T) {
	c := newConnectedClientForTest(t, &scriptedRedisHook{
		hgetReplies: []stringCmdReply{{err: redis.Nil}},
		getReplies:  []stringCmdReply{{err: redis.Nil}},
	})

	cost, err := c.GetDailyCost(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if cost != 0 {
		t.Fatalf("expected cost 0, got %v", cost)
	}
}

func TestGetDailyCost_LegacyGetNonRedisNilPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	c := newConnectedClientForTest(t, &scriptedRedisHook{
		hgetReplies: []stringCmdReply{{err: errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")}},
		getReplies:  []stringCmdReply{{err: wantErr}},
	})

	_, err := c.GetDailyCost(context.Background(), "key-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected err %v, got %v", wantErr, err)
	}
}

func TestGetDailyCost_FallbackToLegacyOnHGetError(t *testing.T) {
	c := newConnectedClientForTest(t, &scriptedRedisHook{
		hgetReplies: []stringCmdReply{{err: errors.New("WRONGTYPE Operation against a key holding the wrong kind of value")}},
		getReplies:  []stringCmdReply{{val: "12.34"}},
	})

	cost, err := c.GetDailyCost(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if cost != 12.34 {
		t.Fatalf("expected cost 12.34, got %v", cost)
	}
}
