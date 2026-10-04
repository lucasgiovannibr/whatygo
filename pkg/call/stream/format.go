package call_stream

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	call_engine "github.com/lucasgiovannibr/whatygo/pkg/call/engine"
)

// The audio encodings a stream can carry. The names are the ones the "start" message
// reports, and the ones Twilio Media Streams and the OpenAI Realtime API use.
const (
	EncodingPCM   = "audio/pcm-s16le" // signed 16-bit little-endian, mono
	EncodingMulaw = "audio/x-mulaw"   // G.711 mu-law, one byte per sample, 8 kHz
	EncodingAlaw  = "audio/x-alaw"    // G.711 A-law, one byte per sample, 8 kHz
)

// ErrInvalidAudioFormat: the encoding or sample rate asked for is not one the stream
// can carry.
var ErrInvalidAudioFormat = errors.New("unsupported audio format")

// AudioFormat is the audio of one stream, as the client sees it. The call itself always
// runs at 16 kHz; the stream converts. The zero value is the default, 16 kHz PCM.
type AudioFormat struct {
	Encoding   string
	SampleRate int
}

// DefaultAudioFormat is what a stream carries unless asked otherwise.
var DefaultAudioFormat = AudioFormat{Encoding: EncodingPCM, SampleRate: call_engine.SampleRate}

var encodingNames = map[string]string{
	"audio/pcm-s16le": EncodingPCM, "pcm": EncodingPCM, "pcm16": EncodingPCM, "pcm_s16le": EncodingPCM, "linear16": EncodingPCM,
	"audio/x-mulaw": EncodingMulaw, "audio/pcmu": EncodingMulaw, "mulaw": EncodingMulaw, "ulaw": EncodingMulaw, "pcmu": EncodingMulaw,
	"audio/x-alaw": EncodingAlaw, "audio/pcma": EncodingAlaw, "alaw": EncodingAlaw, "pcma": EncodingAlaw,
}

// ParseAudioFormat validates what a client asked for. An empty encoding is PCM and a
// zero rate is the usual one for the encoding (16 kHz for PCM, 8 kHz for G.711).
// PCM may be 8, 16 or 24 kHz; mu-law and A-law are 8 kHz only, which is what they are.
func ParseAudioFormat(encoding string, sampleRate int) (AudioFormat, error) {
	name := EncodingPCM
	if e := strings.ToLower(strings.TrimSpace(encoding)); e != "" {
		var ok bool
		if name, ok = encodingNames[e]; !ok {
			return AudioFormat{}, fmt.Errorf("%w: encoding %q (use %s, %s or %s)", ErrInvalidAudioFormat, encoding, EncodingPCM, EncodingMulaw, EncodingAlaw)
		}
	}

	f := AudioFormat{Encoding: name, SampleRate: sampleRate}
	if name == EncodingPCM {
		if f.SampleRate == 0 {
			f.SampleRate = call_engine.SampleRate
		}
		switch f.SampleRate {
		case 8000, 16000, 24000:
			return f, nil
		}
		return AudioFormat{}, fmt.Errorf("%w: %d Hz PCM (use 8000, 16000 or 24000)", ErrInvalidAudioFormat, sampleRate)
	}

	if f.SampleRate == 0 {
		f.SampleRate = 8000
	}
	if f.SampleRate != 8000 {
		return AudioFormat{}, fmt.Errorf("%w: %s is 8000 Hz only", ErrInvalidAudioFormat, name)
	}
	return f, nil
}

// normalized fills in the default of a zero value.
func (f AudioFormat) normalized() AudioFormat {
	if f.Encoding == "" {
		return DefaultAudioFormat
	}
	return f
}

// isDefault: the stream carries what the call carries, so nothing is converted.
func (f AudioFormat) isDefault() bool { return f.normalized() == DefaultAudioFormat }

// converter turns the call's audio (16 kHz float frames) into the stream's format and
// back. Each direction has its own state and is meant for one goroutine: encode for the
// socket writer, decode for the socket reader.
type converter struct {
	f AudioFormat

	down *resampler // call rate -> stream rate; nil when they are the same
	up   *resampler // stream rate -> call rate
	odd  []byte     // a lone byte left over when a PCM chunk had an odd length
}

func newConverter(f AudioFormat) *converter {
	f = f.normalized()
	return &converter{
		f:    f,
		down: newResampler(call_engine.SampleRate, f.SampleRate),
		up:   newResampler(f.SampleRate, call_engine.SampleRate),
	}
}

// encode converts one chunk of the peer's audio into the bytes of a "media" message.
func (c *converter) encode(frame []float32) []byte {
	if c.down != nil {
		frame = c.down.Process(frame)
	}
	switch c.f.Encoding {
	case EncodingMulaw:
		out := make([]byte, len(frame))
		for i, s := range frame {
			out[i] = linearToMulaw(toInt16(s))
		}
		return out
	case EncodingAlaw:
		out := make([]byte, len(frame))
		for i, s := range frame {
			out[i] = linearToAlaw(toInt16(s))
		}
		return out
	}
	out := make([]byte, len(frame)*2)
	for i, s := range frame {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(toInt16(s)))
	}
	return out
}

// decode converts the bytes of a client's "media" message into the call's audio.
func (c *converter) decode(data []byte) []float32 {
	var samples []float32
	switch c.f.Encoding {
	case EncodingMulaw:
		samples = make([]float32, len(data))
		for i, b := range data {
			samples[i] = float32(mulawToLinear(b)) / 32768.0
		}
	case EncodingAlaw:
		samples = make([]float32, len(data))
		for i, b := range data {
			samples[i] = float32(alawToLinear(b)) / 32768.0
		}
	default:
		if len(c.odd) > 0 {
			data = append(c.odd, data...)
			c.odd = nil
		}
		if len(data)%2 == 1 {
			c.odd = []byte{data[len(data)-1]}
			data = data[:len(data)-1]
		}
		samples = make([]float32, len(data)/2)
		for i := range samples {
			samples[i] = float32(int16(binary.LittleEndian.Uint16(data[2*i:]))) / 32768.0
		}
	}
	if c.up != nil {
		return c.up.Process(samples)
	}
	return samples
}

// toInt16 converts a float sample to 16 bits, clamping like a recorder would.
func toInt16(s float32) int16 {
	v := s * 32768.0
	switch {
	case v > 32767:
		return 32767
	case v < -32768:
		return -32768
	}
	return int16(v)
}

// G.711, the telephone codecs. These are the classic table-free formulations; the
// tests check them against known values and a round trip.

const (
	mulawBias = 0x84
	mulawClip = 32635
)

func linearToMulaw(s int16) byte {
	v := int(s)
	sign := 0
	if v < 0 {
		v, sign = -v, 0x80
	}
	if v > mulawClip {
		v = mulawClip
	}
	v += mulawBias
	exp := 7
	for mask := 0x4000; v&mask == 0 && exp > 0; mask >>= 1 {
		exp--
	}
	mant := (v >> (exp + 3)) & 0x0F
	return ^byte(sign | exp<<4 | mant)
}

func mulawToLinear(u byte) int16 {
	u = ^u
	exp := int(u>>4) & 7
	v := ((int(u&0x0F) << 3) + mulawBias) << exp
	v -= mulawBias
	if u&0x80 != 0 {
		return int16(-v)
	}
	return int16(v)
}

var alawSegmentEnd = [8]int{0x1F, 0x3F, 0x7F, 0xFF, 0x1FF, 0x3FF, 0x7FF, 0xFFF}

func linearToAlaw(s int16) byte {
	v := int(s) >> 3 // A-law works on 13 bits
	mask := 0xD5
	if v < 0 {
		mask = 0x55
		v = -v - 1
	}
	seg := 0
	for seg < 8 && v > alawSegmentEnd[seg] {
		seg++
	}
	if seg >= 8 {
		return byte(0x7F ^ mask)
	}
	a := seg << 4
	if seg < 2 {
		a |= (v >> 1) & 0x0F
	} else {
		a |= (v >> seg) & 0x0F
	}
	return byte(a ^ mask)
}

func alawToLinear(a byte) int16 {
	a ^= 0x55
	t := int(a&0x0F) << 4
	seg := int(a&0x70) >> 4
	switch seg {
	case 0:
		t += 8
	case 1:
		t += 0x108
	default:
		t += 0x108
		t <<= seg - 1
	}
	if a&0x80 != 0 {
		return int16(t)
	}
	return int16(-t)
}
