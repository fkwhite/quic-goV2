package quic

import (
	// "fmt"
	"sync"
)

type StreamBuffer struct {
	mtxs    []sync.Mutex
	buffers []float64
	// len int
}

var globalBuffers StreamBuffer

func GlobalBuffersInit(numStreams int) {
	globalBuffers.mtxs = make([]sync.Mutex, numStreams)
	globalBuffers.buffers = make([]float64, numStreams)
	// globalBuffers.len = numStreams
}

// delta can be positive or negative
func GlobalBuffersIncr(streamIdx int, delta float64) {
	globalBuffers.mtxs[streamIdx].Lock()
	defer globalBuffers.mtxs[streamIdx].Unlock()
	globalBuffers.buffers[streamIdx] += delta
}

func GlobalBuffersRead(streamIdx int) float64 {
	// if (streamIdx >= globalBuffers.len){
	// 	fmt.Printf("Error in stream idx %d, max len %d \n", streamIdx, globalBuffers.len)
	// }
	globalBuffers.mtxs[streamIdx].Lock()
	defer globalBuffers.mtxs[streamIdx].Unlock()
	return globalBuffers.buffers[streamIdx]
}

func GlobalBuffersWrite(streamIdx int, newVal float64) {
	globalBuffers.mtxs[streamIdx].Lock()
	defer globalBuffers.mtxs[streamIdx].Unlock()
	globalBuffers.buffers[streamIdx] = newVal
}
