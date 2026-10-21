// 测试用：模拟 GPT-SoVITS 的 / 接口，返回 1.5 秒 440Hz 的 wav
package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
)

func wav(rate, ms int) []byte {
	n := rate * ms / 1000
	data := make([]byte, 44+n*2)
	copy(data[0:], "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(36+n*2))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1) // PCM
	binary.LittleEndian.PutUint16(data[22:], 1) // mono
	binary.LittleEndian.PutUint32(data[24:], uint32(rate))
	binary.LittleEndian.PutUint32(data[28:], uint32(rate*2))
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(n*2))
	for i := 0; i < n; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/float64(rate)))
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(v))
	}
	return data
}

func main() {
	audio := wav(16000, 1500)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]any
		json.Unmarshal(body, &req)
		fmt.Printf("mock TTS: text=%q lang=%v ref=%v\n",
			req["text"], req["text_language"], req["refer_wav_path"])
		w.Header().Set("Content-Type", "audio/wav")
		w.Write(audio)
	})
	fmt.Println("mock tts listening on 127.0.0.1:8898")
	http.ListenAndServe("127.0.0.1:8898", nil)
}
