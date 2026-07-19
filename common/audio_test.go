package common

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type boundedAudioReadSeeker struct {
	*bytes.Reader
	maxRead int
	reads   int
}

func (r *boundedAudioReadSeeker) Read(data []byte) (int, error) {
	r.reads++
	if len(data) > r.maxRead {
		r.maxRead = len(data)
	}
	return r.Reader.Read(data)
}

func adtsTestFrame(sampleIndex byte, payload []byte) []byte {
	frameLength := len(payload) + 7
	header := []byte{
		0xff,
		0xf1,
		(1 << 6) | (sampleIndex << 2),
		byte(frameLength >> 11),
		byte(frameLength >> 3),
		byte(frameLength<<5) | 0x1f,
		0xfc,
	}
	return append(header, payload...)
}

func TestGetAACDurationScansFramesWithConstantMemory(t *testing.T) {
	data := append(adtsTestFrame(4, bytes.Repeat([]byte{1}, 64)), adtsTestFrame(4, bytes.Repeat([]byte{2}, 64))...)
	reader := &boundedAudioReadSeeker{Reader: bytes.NewReader(data)}

	duration, err := getAACDuration(reader)

	require.NoError(t, err)
	assert.InDelta(t, float64(2048)/44100, duration, 0.000001)
	assert.LessOrEqual(t, reader.maxRead, 7, "AAC duration parsing must not allocate a body-sized read buffer")
	assert.Equal(t, 2, reader.reads)
}

func TestGetAACDurationRejectsTruncatedFrameBeforeCountingIt(t *testing.T) {
	frame := adtsTestFrame(4, bytes.Repeat([]byte{1}, 64))
	frame = frame[:7]
	reader := &boundedAudioReadSeeker{Reader: bytes.NewReader(frame)}

	_, err := getAACDuration(reader)

	require.ErrorContains(t, err, "truncated aac frame payload")
	assert.Equal(t, 1, reader.reads)
}

func TestGetAACDurationRejectsLargeUnsynchronizedPrefixImmediately(t *testing.T) {
	reader := &boundedAudioReadSeeker{Reader: bytes.NewReader(bytes.Repeat([]byte{'x'}, 1<<20))}

	_, err := getAACDuration(reader)

	require.ErrorContains(t, err, "invalid aac frame sync word")
	assert.Equal(t, 1, reader.reads, "invalid input must not trigger a byte-by-byte seek/read scan")
}

func TestGetAudioDurationHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := GetAudioDuration(ctx, bytes.NewReader(adtsTestFrame(4, []byte{1})), ".aac")

	require.ErrorIs(t, err, context.Canceled)
}

type cancelingAudioReadSeeker struct {
	*bytes.Reader
	cancel context.CancelFunc
}

func (r *cancelingAudioReadSeeker) Read(data []byte) (int, error) {
	n, err := r.Reader.Read(data)
	r.cancel()
	return n, err
}

func TestGetAudioDurationChecksContextDuringParsing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelingAudioReadSeeker{
		Reader: bytes.NewReader(append(adtsTestFrame(4, []byte{1}), adtsTestFrame(4, []byte{2})...)),
		cancel: cancel,
	}

	_, err := GetAudioDuration(ctx, reader, ".aac")

	require.ErrorIs(t, err, context.Canceled)
}

func TestGetAudioDurationRejectsConfiguredMaximum(t *testing.T) {
	t.Setenv("RELAY_AUDIO_DURATION_MAX_SECONDS", "1")
	frame := adtsTestFrame(4, nil)
	data := bytes.Repeat(frame, 44)

	_, err := GetAudioDuration(context.Background(), bytes.NewReader(data), ".aac")

	require.ErrorContains(t, err, "invalid audio duration")
}

func TestGetM4ADurationRejectsZeroTimescale(t *testing.T) {
	_, err := getM4ADuration(bytes.NewReader(nil))

	require.ErrorContains(t, err, "timescale")
}
