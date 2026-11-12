//go:build windows

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestMatchesPlaybackEndRejectsStaleStream(t *testing.T) {
	e := &AudioEngine{stream: 42, streamGeneration: 7}
	if !e.MatchesPlaybackEnd(7, 42) {
		t.Fatal("current playback end should match")
	}
	if e.MatchesPlaybackEnd(6, 42) {
		t.Fatal("stale generation should not match")
	}
	if e.MatchesPlaybackEnd(7, 41) {
		t.Fatal("stale stream handle should not match")
	}
	if (&AudioEngine{}).MatchesPlaybackEnd(0, 0) {
		t.Fatal("empty engine should not accept an end notification")
	}
}

func TestAudioEngineHotSwapSmoke(t *testing.T) {
	if os.Getenv("AURALUX_AUDIO_SMOKE") != "1" {
		t.Skip("set AURALUX_AUDIO_SMOKE=1 to exercise the installed BASS runtime")
	}

	path := filepath.Join(t.TempDir(), "silence.wav")
	if err := os.WriteFile(path, silentWAV(48000, 2, 12000), 0600); err != nil {
		t.Fatal(err)
	}

	e := newAudioEngine()
	if err := e.Load(filepath.Join("runtime"), 0); err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.SetVolume(0)

	if err := e.ReplaceAndPlay(path, false); err != nil {
		t.Fatal(err)
	}
	firstGeneration := e.streamGeneration
	if !e.IsPlaying() {
		t.Fatal("first prepared stream did not start")
	}
	if err := e.ReplaceAndPlay(path, false); err != nil {
		t.Fatal(err)
	}
	if e.streamGeneration <= firstGeneration {
		t.Fatal("hot swap did not install a new stream generation")
	}
	if !e.IsPlaying() {
		t.Fatal("replacement stream did not start")
	}
}

func silentWAV(sampleRate, channels, frames int) []byte {
	dataBytes := frames * channels * 2
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+dataBytes))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(16))
	_ = binary.Write(&b, binary.LittleEndian, uint16(1))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate))
	_ = binary.Write(&b, binary.LittleEndian, uint32(sampleRate*channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels*2))
	_ = binary.Write(&b, binary.LittleEndian, uint16(16))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(dataBytes))
	b.Write(make([]byte, dataBytes))
	return b.Bytes()
}
