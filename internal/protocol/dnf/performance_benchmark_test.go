package dnf

import (
	"testing"

	"robot/internal/protocol/dnf/crypt"
)

func BenchmarkMessageDispatchShardCursor(b *testing.B) {
	shard := newMessageDispatchShard()
	msg := MsgQueueData{Type: "benchmark"}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		shard.queue = append(shard.queue, msg)
		shard.queue[shard.head] = MsgQueueData{}
		shard.head++
		shard.compactIfNeeded(false)
		if shard.head == len(shard.queue) {
			shard.compactIfNeeded(true)
		}
	}
}

func BenchmarkBuildSendPacket(b *testing.B) {
	cipher := crypt.NewDNFCipher()
	if err := cipher.Initialize(make([]byte, 334)); err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 256)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	for i := 0; i < b.N; i++ {
		if _, err := buildSendPacket(37, uint16(i), payload, cipher); err != nil {
			b.Fatal(err)
		}
	}
}
