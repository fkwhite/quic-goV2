package quic

import (
	"errors"
	"sync"
	"fmt"
	"os"
	"encoding/json"

	"github.com/lucas-clemente/quic-go/internal/ackhandler"
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	"github.com/lucas-clemente/quic-go/quicvarint"
)

type Configuration struct {
	Weight   		[]float64
	Penalty   	[]float64
}


type framer interface {
	HasData() bool

	QueueControlFrame(wire.Frame)
	AppendControlFrames([]ackhandler.Frame, protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount)

	AddActiveStream(protocol.StreamID)
	SchedulerProposalQueuing([]ackhandler.Frame, protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) //CRIS
	SchedulerWFQ([]ackhandler.Frame, protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) //CRIS
	SchedulerFairQueuing([]ackhandler.Frame, protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) //CRIS
	AppendStreamFrames([]ackhandler.Frame, protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount)

	Handle0RTTRejection() error

	AdvancedOutgoingStreamManagement
}

type framerI struct {
	mutex sync.Mutex

	streamGetter streamGetter
	version      protocol.VersionNumber

	activeStreams map[protocol.StreamID]struct{}
	streamQueue   []protocol.StreamID

	controlFrameMutex sync.Mutex
	controlFrames     []wire.Frame
}

var _ framer = &framerI{}

func newFramer(
	streamGetter streamGetter,
	v protocol.VersionNumber,
) framer {
	return &framerI{
		streamGetter:  streamGetter,
		activeStreams: make(map[protocol.StreamID]struct{}),
		version:       v,
	}
}

func (f *framerI) HasData() bool {
	f.mutex.Lock()
	hasData := len(f.streamQueue) > 0
	f.mutex.Unlock()
	if hasData {
		return true
	}
	f.controlFrameMutex.Lock()
	hasData = len(f.controlFrames) > 0
	f.controlFrameMutex.Unlock()
	return hasData
}

func (f *framerI) QueueControlFrame(frame wire.Frame) {
	f.controlFrameMutex.Lock()
	f.controlFrames = append(f.controlFrames, frame)
	f.controlFrameMutex.Unlock()
}

func (f *framerI) AppendControlFrames(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	f.controlFrameMutex.Lock()
	for len(f.controlFrames) > 0 {
		frame := f.controlFrames[len(f.controlFrames)-1]
		frameLen := frame.Length(f.version)
		if length+frameLen > maxLen {
			break
		}
		frames = append(frames, ackhandler.Frame{Frame: frame})
		length += frameLen
		f.controlFrames = f.controlFrames[:len(f.controlFrames)-1]
	}
	f.controlFrameMutex.Unlock()
	return frames, length
}

func (f *framerI) AddActiveStream(id protocol.StreamID) {
	f.mutex.Lock()
	if _, ok := f.activeStreams[id]; !ok {
		f.streamQueue = append(f.streamQueue, id)
		f.activeStreams[id] = struct{}{}
	}
	f.mutex.Unlock()
}



//Added
func Min(array []protocol.ByteCount) (int) {
	var min protocol.ByteCount = array[0]
	var id_stream int
	
	for id, value := range array {
			if min > value {
					min = value
					id_stream = id
			}
	}
	return id_stream
}

//TODO: scheduler --> Proposal Queuing
func (f *framerI) SchedulerProposalQueuing(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame

	configFile := "conf_Scheduler.json" //FIXME: cambiar 
	file, err := os.Open(configFile)
	if err != nil {
		fmt.Println("An error has ocurred -- Opening configuration file")
		panic(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		fmt.Println("error:", err)
	}

	f.mutex.Lock()

	// pop STREAM frames, until less than MinStreamFrameSize bytes are left in the packet
	numActiveStreams := len(f.streamQueue)
	fmt.Println("\n Streams Activos: ", numActiveStreams)
	fmt.Println("Maxima Longitud Bytes: ", maxLen, "    Minimo: ", protocol.MinStreamFrameSize)

	// Quien tenga mas prioridad es quien envia (toda la C posible)
	// Si no tiene bytes suficientes en el buffer, se envian del siguiente stream con mayor prioridad
	// La prioridad se mira en función del número de bytes en la cola (mayor num bytes, mayor prioridad) y los pesos de cada stream

	r := make([]protocol.ByteCount, numActiveStreams)
	
	for i := 0; i < numActiveStreams; i++ {
		id := f.streamQueue[i] 
		str, _ := f.streamGetter.GetOrOpenSendStream(id)
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame
		fmt.Println("+++++++++++++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX)
		r[i] =  totalTX
	}

	var sum_r protocol.ByteCount
	for i := 0; i < len(r); i++ {
		sum_r += r[i]
	}

	C := maxLen - protocol.MinStreamFrameSize
	OUT := make([]protocol.ByteCount, numActiveStreams)
	priority := make([]protocol.ByteCount, numActiveStreams)

	if sum_r < C {
		OUT = r
	} else {
		for i := 0; i < numActiveStreams; i++ {
			id := f.streamQueue[i]/4
			priority[i] = protocol.ByteCount(configuration.Penalty[id]) - r[i]
		}
		id_stream := Min(priority)
		newStream := true

		for newStream {
			if C <= r[id_stream] {
				OUT[id_stream] = C
				newStream = false
			} else {
				//Si el menor es r[i] --> enviar un nuevo stream con los bytes sobrantes
				OUT[id_stream] = r[id_stream]
				priority[id_stream] = 0 // no estoy segura....
				C -= r[id_stream]
				id_stream = Min(priority)
				// newStream = true
			}
		}
	}
	fmt.Println("OUT = ", OUT, "   ID order: ", f.streamQueue) 

	for i := 0; i < numActiveStreams; i++ {
		id := f.streamQueue[0] // El ID va de 4 en 4 [0, 4, 8, 12] 
		// fmt.Println("Stream id: ", id) 
		f.streamQueue = f.streamQueue[1:] 
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		bufferTx := str.RemainingBytes() 
		bufferRtx := str.BytesToRetransmit()
		fmt.Println("+++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		// minSize := protocol.ByteCount(7)
		// if totalTX < minSize || OUT[i] < minSize { 
		// 	fmt.Println("Tamaño pequeño frame")
		// 	f.streamQueue = append(f.streamQueue, id)
		// 	continue
		// }

		//TODO:
		if OUT[i]==0 || protocol.MinStreamFrameSize+length > maxLen {
			// break
			fmt.Println("Tamaño pequeño frame")
			f.streamQueue = append(f.streamQueue, id)
			continue
		}

		bytesToTransmit := OUT[i]
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		bytesToTransmit += quicvarint.Len(uint64(bytesToTransmit)) 
		fmt.Println("Datos a enviar: ", bytesToTransmit) 
		
		frame, hasMoreData := str.popStreamFrame(bytesToTransmit)
		lengthFrame := frame.Length(f.version)
		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		length += lengthFrame
		fmt.Println("Datos enviados realmente (parte 1): ", lengthFrame) 

		totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		bufferTx = str.RemainingBytes() 
		bufferRtx = str.BytesToRetransmit()
		fmt.Println("-----------1 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		for lengthFrame < bytesToTransmit  && protocol.MinStreamFrameSize+length <= maxLen {
			bytesToTransmit -= lengthFrame
			frame, hasMoreData = str.popStreamFrame(bytesToTransmit)
			lengthFrame = frame.Length(f.version)
			if frame == nil {
				continue
			}
			frames = append(frames, *frame)
			length += lengthFrame
			fmt.Println("Datos enviados realmente (parte 2): ", lengthFrame) 
		}

		totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		bufferTx = str.RemainingBytes() 
		bufferRtx = str.BytesToRetransmit()
		fmt.Println("-----------2 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		if hasMoreData { // put the stream back in the queue (at the end)
			f.streamQueue = append(f.streamQueue, id)
		} else { // no more data to send. Stream is not active any more
			fmt.Println("No more data") 
			delete(f.activeStreams, id)
		}
		fmt.Println("ID order (despues): ", f.streamQueue) 

		lastFrame = frame
	}
	
	f.mutex.Unlock()
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	fmt.Println("Datos enviados (TOTAL): ", length) 
	return frames, length
}


//Added
func (f *framerI) orderOUT_WFQ(numActiveStreams int, C protocol.ByteCount, totalWeight float64,  r []protocol.ByteCount, OUT []protocol.ByteCount, configuration Configuration) ([]protocol.ByteCount, protocol.ByteCount, float64, bool) {
	diff := protocol.ByteCount(0)
	hasExcess := false
	weight := totalWeight

	for i := 0; i < numActiveStreams; i++ {
		if OUT[i] == r[i] {
			continue
		}

		id := f.streamQueue[i]/4
		OUT[i] += protocol.ByteCount(configuration.Weight[id] * float64(C) / totalWeight)
		// fmt.Println("ID: ", id, "   OUT: ", OUT[i])
		
		if OUT[i] > r[i] {
			diff += (OUT[i] - r[i]) 
			weight -= configuration.Weight[id]
			OUT[i] = r[i] 
			// fmt.Println("ID: ", id, "   diff: ", diff, "   OUT: ", OUT[i])
			hasExcess = true
		}
		
	}
	return OUT, diff, weight, hasExcess
}

//TODO: scheduler --> WFQ
func (f *framerI) SchedulerWFQ(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame

	configFile := "conf_Scheduler.json"
	file, err := os.Open(configFile)
	if err != nil {
		fmt.Println("An error has ocurred -- Opening configuration file")
		panic(err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	configuration := Configuration{}
	err = decoder.Decode(&configuration)
	if err != nil {
		fmt.Println("error:", err)
	}
	
	totalWeight := float64(0)
	for i := 0; i < len(configuration.Weight); i++ {
		totalWeight += configuration.Weight[i]
	}

	f.mutex.Lock()

	numActiveStreams := len(f.streamQueue)
	// fmt.Println("\n Streams Activos: ", numActiveStreams)
	// fmt.Println("Maxima Longitud Bytes: ", maxLen, "    Minimo: ", protocol.MinStreamFrameSize)

	r := make([]protocol.ByteCount, numActiveStreams)
	
	for i := 0; i < numActiveStreams; i++ {
		id := f.streamQueue[i] 
		str, _ := f.streamGetter.GetOrOpenSendStream(id)
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame
		// fmt.Println("+++++++++++++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX)
		r[i] =  totalTX
	}
	var sum_r protocol.ByteCount
	for i := 0; i < len(r); i++ {
		sum_r += r[i]
	}

	C := maxLen - protocol.MinStreamFrameSize
	OUT := make([]protocol.ByteCount, numActiveStreams)
	var hasExcess bool

	if sum_r < C {
		OUT = r
	} else {
		OUT, C, totalWeight, hasExcess= f.orderOUT_WFQ(numActiveStreams, C , totalWeight,  r , OUT , configuration)
		// fmt.Println("OUT (antes diff) = ", OUT, "   ID order: ", f.streamQueue) 
		for hasExcess {
			OUT, C, totalWeight, hasExcess= f.orderOUT_WFQ(numActiveStreams, C , totalWeight,  r , OUT , configuration)
		}
		
	}
	
	// fmt.Println("OUT = ", OUT, "   ID order: ", f.streamQueue) 

	for i := 0; i < numActiveStreams; i++ {
		// pruebaLen := protocol.ByteCount(0)
		id := f.streamQueue[0] // El ID va de 4 en 4 [0, 4, 8, 12] 
		// fmt.Println("Stream id: ", id) 
		f.streamQueue = f.streamQueue[1:] 
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx := str.RemainingBytes() 
		// bufferRtx := str.BytesToRetransmit()
		// fmt.Println("+++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		minSize := protocol.ByteCount(7) //protocol.ByteCount(100) //protocol.MinStreamFrameSize //FIXME:
		if totalTX < minSize || OUT[i] < minSize { 
			// fmt.Println("Tamaño pequeño frame")
			f.streamQueue = append(f.streamQueue, id)
			continue
		}

		bytesToTransmit := OUT[i]
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		bytesToTransmit += quicvarint.Len(uint64(bytesToTransmit)) 
		// fmt.Println("Datos a enviar: ", bytesToTransmit) 
		
		frame, hasMoreData := str.popStreamFrame(bytesToTransmit)
		lengthFrame := frame.Length(f.version)
		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		length += lengthFrame
		// pruebaLen += lengthFrame
		// fmt.Println("Datos enviados realmente: ", lengthFrame) 

		// totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx = str.RemainingBytes() 
		// bufferRtx = str.BytesToRetransmit()
		// fmt.Println("-----------1 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		for lengthFrame < bytesToTransmit  && (bytesToTransmit-lengthFrame) >= minSize {
			bytesToTransmit -= lengthFrame
			frame, hasMoreData = str.popStreamFrame(bytesToTransmit)
			lengthFrame = frame.Length(f.version)
			if frame == nil {
				continue
			}
			frames = append(frames, *frame)
			length += lengthFrame
			// pruebaLen += lengthFrame
			// fmt.Println("Datos enviados realmente (parte 2): ", lengthFrame) 
		}

		// totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx = str.RemainingBytes() 
		// bufferRtx = str.BytesToRetransmit()
		// fmt.Println("-----------2 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		if hasMoreData { // put the stream back in the queue (at the end)
			f.streamQueue = append(f.streamQueue, id)
		} else { // no more data to send. Stream is not active any more
			// fmt.Println("No more data") 
			delete(f.activeStreams, id)
		}
		// fmt.Println("ID order (despues): ", f.streamQueue) 

		// fmt.Println("ID: ", id ,"   Datos enviados realmente: ", pruebaLen) 

		lastFrame = frame
	}
	
	f.mutex.Unlock()
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	// fmt.Println("Datos enviados (TOTAL): ", length) 
	return frames, length
}


//Added
func orderOUT(numActiveStreams int, C protocol.ByteCount, N float64, bytesToDistribute float64, r []protocol.ByteCount, OUT []protocol.ByteCount) ([]protocol.ByteCount, protocol.ByteCount, float64, bool) {
	hasExcess := false

	for i := 0; i < numActiveStreams; i++ {
		if float64(r[i]) < bytesToDistribute {
			C -= r[i]
			N -= 1
			if r[i] > 0 {
				OUT[i] += r[i]
				hasExcess = true
			}
		} else {
			C -= protocol.ByteCount(bytesToDistribute) 
			OUT[i] += protocol.ByteCount(bytesToDistribute)
		}
	}

	return OUT, C, N, hasExcess
}

//TODO: scheduler --> FairQueuing
func (f *framerI) SchedulerFairQueuing(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame
	f.mutex.Lock()

	// pop STREAM frames, until less than MinStreamFrameSize bytes are left in the packet
	numActiveStreams := len(f.streamQueue)
	// fmt.Println("\n Streams Activos: ", numActiveStreams)
	// fmt.Println("Maxima Longitud Bytes: ", maxLen, "    Minimo: ", protocol.MinStreamFrameSize)

	r := make([]protocol.ByteCount, numActiveStreams)
	
	for i := 0; i < numActiveStreams; i++ {
		id := f.streamQueue[i] 
		str, _ := f.streamGetter.GetOrOpenSendStream(id)
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame
		// fmt.Println("+++++++++++++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX)
		r[i] =  totalTX
	}
	var sum_r protocol.ByteCount
	for i := 0; i < len(r); i++ {
		sum_r += r[i]
	}

	C := maxLen - protocol.MinStreamFrameSize
	N := float64(numActiveStreams)
	bytesToDistribute := float64(C)/N
	OUT := make([]protocol.ByteCount, numActiveStreams)
	var hasExcess bool
	
	if sum_r < C {
		OUT = r
	} else {
		OUT, C, N, hasExcess = orderOUT(numActiveStreams, C, N, bytesToDistribute, r, OUT) 
		for hasExcess {	
			for i := 0; i < len(r); i++ {
				r[i] -= OUT[i]
			}
			bytesToDistribute = float64(C)/N
			N = float64(numActiveStreams)
			OUT, C, N, hasExcess = orderOUT(numActiveStreams, C, N, bytesToDistribute, r, OUT) 
		}
	}
	// fmt.Println("OUT = ", OUT, "   ID order: ", f.streamQueue) 

	for i := 0; i < numActiveStreams; i++ {
		// pruebaLen := protocol.ByteCount(0)
		id := f.streamQueue[0] // El ID va de 4 en 4 [0, 4, 8, 12] 
		// fmt.Println("Stream id: ", id) 
		f.streamQueue = f.streamQueue[1:] 
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}
		totalTX := str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx := str.RemainingBytes() 
		// bufferRtx := str.BytesToRetransmit()
		// fmt.Println("+++++++++++ ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		minSize := protocol.ByteCount(7) //protocol.ByteCount(100) //protocol.MinStreamFrameSize //FIXME:
		if totalTX < minSize || OUT[i] < minSize { 
			// fmt.Println("Tamaño pequeño frame")
			f.streamQueue = append(f.streamQueue, id)
			continue
		}

		bytesToTransmit := OUT[i]
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		bytesToTransmit += quicvarint.Len(uint64(bytesToTransmit)) 
		// fmt.Println("Datos a enviar: ", bytesToTransmit) 
		
		frame, hasMoreData := str.popStreamFrame(bytesToTransmit)
		lengthFrame := frame.Length(f.version)
		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		length += lengthFrame
		// pruebaLen += lengthFrame
		// fmt.Println("Datos enviados realmente (parte 1): ", lengthFrame) 

		// totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx = str.RemainingBytes() 
		// bufferRtx = str.BytesToRetransmit()
		// fmt.Println("-----------1 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		for lengthFrame < bytesToTransmit  && (bytesToTransmit-lengthFrame) >= minSize {
			bytesToTransmit -= lengthFrame
			frame, hasMoreData = str.popStreamFrame(bytesToTransmit)
			lengthFrame = frame.Length(f.version)
			if frame == nil {
				continue
			}
			frames = append(frames, *frame)
			length += lengthFrame
			// pruebaLen += lengthFrame
			// fmt.Println("Datos enviados realmente (parte 2): ", lengthFrame) 
		}

		// totalTX = str.TotalQueue() // bufferTx + bufferRtx + bufferNextFrame 
		// bufferTx = str.RemainingBytes() 
		// bufferRtx = str.BytesToRetransmit()
		// fmt.Println("-----------2 ID: ", id ,"   NumBytes Total: ", totalTX, "   NumB TX: ", bufferTx, "   NumB RTX: ", bufferRtx)

		if hasMoreData { // put the stream back in the queue (at the end)
			f.streamQueue = append(f.streamQueue, id)
		} else { // no more data to send. Stream is not active any more
			// fmt.Println("No more data") 
			delete(f.activeStreams, id)
		}
		// fmt.Println("ID order (despues): ", f.streamQueue) 

		// fmt.Println("ID: ", id ,"   Datos enviados realmente (total): ", pruebaLen) 

		lastFrame = frame
	}
	
	f.mutex.Unlock()
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	// fmt.Println("Datos enviados (TOTAL): ", length) 
	return frames, length
}


/*
func (f *framerI) AppendStreamFrames(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame
	f.mutex.Lock()
	// pop STREAM frames, until less than MinStreamFrameSize bytes are left in the packet
	numActiveStreams := len(f.streamQueue)
	// fmt.Println("\n Streams Activos: ", numActiveStreams) //CRIS

	for i := 0; i < numActiveStreams; i++ {
		if protocol.MinStreamFrameSize+length > maxLen {
			break
		}
		id := f.streamQueue[0]
		// fmt.Println("Stream id: ", id) 
		f.streamQueue = f.streamQueue[1:]
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}

		// //CRIS
		// bufferTx := str.RemainingBytes() 
		// bufferRtx := str.BytesToRetransmit()
		// bufferTotal := (bufferTx + bufferRtx)
		// totalTX := str.TotalQueue()
		// fmt.Println("+++++++++ ID: ", id ,"   NumB cola TX: ", bufferTx, "   NumB cola RTX: ", bufferRtx, "   NumB Total: ", bufferTotal)
		// fmt.Println("++++++++++ TOTAL TX ", totalTX)
		// //CRIS

		remainingLen := maxLen - length
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		remainingLen += quicvarint.Len(uint64(remainingLen)) //parece que siempre son 2B
		// fmt.Println("Remaining Length:  ", remainingLen, "   MaxLen: ", maxLen, "   Length: ", length)

		frame, hasMoreData := str.popStreamFrame(remainingLen)
		if hasMoreData { // put the stream back in the queue (at the end)
			f.streamQueue = append(f.streamQueue, id)
		} else { // no more data to send. Stream is not active any more
			delete(f.activeStreams, id)
		}
		// fmt.Println("Cola Stream Despues:  ", f.streamQueue)
		
		// bufferTx = str.RemainingBytes() 
		// bufferRtx = str.BytesToRetransmit()
		// bufferTotal = (bufferTx + bufferRtx)
		// totalTX = str.TotalQueue()
		// fmt.Println("---------- ID: ", id ,"   NumB cola TX: ", bufferTx, "   NumB cola RTX: ", bufferRtx, "   NumB Total: ", bufferTotal)
		// fmt.Println("----------- TOTAL TX ", totalTX)
		// //CRIS

		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		intermedio := frame.Length(f.version)
		length += intermedio
		// aux := frame.Frame
		// fmt.Println("Datos enviados (length frame): ", intermedio) 
		lastFrame = frame
	}
	f.mutex.Unlock() 
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	return frames, length
}
*/

func (f *framerI) AppendStreamFrames(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame
	f.mutex.Lock()
	// pop STREAM frames, until less than MinStreamFrameSize bytes are left in the packet
	numActiveStreams := len(f.streamQueue)
	for i := 0; i < numActiveStreams; i++ {
		if protocol.MinStreamFrameSize+length > maxLen {
			break
		}
		id := f.streamQueue[0]
		f.streamQueue = f.streamQueue[1:]
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}
		remainingLen := maxLen - length
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		remainingLen += quicvarint.Len(uint64(remainingLen))
		frame, hasMoreData := str.popStreamFrame(remainingLen)
		if hasMoreData { // put the stream back in the queue (at the end)
			f.streamQueue = append(f.streamQueue, id)
		} else { // no more data to send. Stream is not active any more
			delete(f.activeStreams, id)
		}
		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		length += frame.Length(f.version)
		lastFrame = frame
	}
	f.mutex.Unlock()
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	return frames, length
}




func (f *framerI) Handle0RTTRejection() error {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.controlFrameMutex.Lock()
	f.streamQueue = f.streamQueue[:0]
	for id := range f.activeStreams {
		delete(f.activeStreams, id)
	}
	var j int
	for i, frame := range f.controlFrames {
		switch frame.(type) {
		case *wire.MaxDataFrame, *wire.MaxStreamDataFrame, *wire.MaxStreamsFrame:
			return errors.New("didn't expect MAX_DATA / MAX_STREAM_DATA / MAX_STREAMS frame to be sent in 0-RTT")
		case *wire.DataBlockedFrame, *wire.StreamDataBlockedFrame, *wire.StreamsBlockedFrame:
			continue
		default:
			f.controlFrames[j] = f.controlFrames[i]
			j++
		}
	}
	f.controlFrames = f.controlFrames[:j]
	f.controlFrameMutex.Unlock()
	return nil
}

// HasAppData checks if there is more application data to be transmitted
func (f *framerI) HasAppData() bool {
	return f.streamGetter.HasAppData()
}

// SentBytes returns the number of sent bytes without counting the retransmissions
func (f *framerI) SentBytes() protocol.ByteCount {
	return f.streamGetter.SentBytes()
}

// RemainingBytes returns current length of the application data not transmitted yet
func (f *framerI) RemainingBytes() protocol.ByteCount {
	return f.streamGetter.RemainingBytes()
}

// HasRetransmission checks if any data frame has been enqueued for retransmission
func (f *framerI) HasRetransmission() bool {
	return f.streamGetter.HasRetransmission()
}

// TotalQueue returns current length of the application data not transmitted + retransmitted
// If there are bytes in the next frame, it adds it up and returns it
func (f *framerI) TotalQueue() protocol.ByteCount {
	return f.streamGetter.TotalQueue()
}

// FramesToRetransmit returns the number of frames enqueued for retransmission
func (f *framerI) FramesToRetransmit() int {
	return f.streamGetter.FramesToRetransmit()
}

// BytesToRetransmit returns the number of data bytes excluding frame headers
func (f *framerI) BytesToRetransmit() protocol.ByteCount {
	return f.streamGetter.BytesToRetransmit()
}




/* CRIS
Cambios hechos para que envie con prioridad el primer stream (si el 0 no tiene datos, envia el 4)
Mira en la cola del buffer el numero de bytes (si el 4 tiene mas bytes en cola, le da prioridad)

func (f *framerI) ScheduleStreamFrames(frames []ackhandler.Frame, maxLen protocol.ByteCount) ([]ackhandler.Frame, protocol.ByteCount) {
	var length protocol.ByteCount
	var lastFrame *ackhandler.Frame
	f.mutex.Lock()
	// pop STREAM frames, until less than MinStreamFrameSize bytes are left in the packet
	numActiveStreams := len(f.streamQueue)
	fmt.Println("\n Streams Activos: ", numActiveStreams) //CRIS
	for i := 0; i < numActiveStreams; i++ {
		if protocol.MinStreamFrameSize+length > maxLen {
			break
		}
		
		id := f.streamQueue[0] // El ID va de 4 en 4 [0, 4, 8, 12] 
		fmt.Println("Stream id: ", id) 
		f.streamQueue = f.streamQueue[1:] 
		// This should never return an error. Better check it anyway.
		// The stream will only be in the streamQueue, if it enqueued itself there.
		str, err := f.streamGetter.GetOrOpenSendStream(id)
		// The stream can be nil if it completed after it said it had data.
		if str == nil || err != nil {
			delete(f.activeStreams, id)
			continue
		}
		remainingLen := maxLen - length
		// For the last STREAM frame, we'll remove the DataLen field later.
		// Therefore, we can pretend to have more bytes available when popping
		// the STREAM frame (which will always have the DataLen set).
		remainingLen += quicvarint.Len(uint64(remainingLen))
		frame, hasMoreData := str.popStreamFrame(remainingLen)
		fmt.Println("Cola Stream Antes:  ",f.streamQueue)

		if hasMoreData { // put the stream back in the queue (at the end)
			if id == 0 {
				f.streamQueue, err = insert(f.streamQueue, 0, id)
				if err != nil {
					fmt.Println(err)
				}
			} else { //if id == 4
				f.streamQueue = append(f.streamQueue, id)
			// 	//queremos dar mas prioridad al 2do que al 3ro ->
			// 	position := 1
			// 	if len(f.streamQueue) == 0 || f.streamQueue[0] != 0 {
			// 		position = 0
			// 	} 
			// 	f.streamQueue, err = insert(f.streamQueue, position, id)
			// 	if err != nil {
			// 		fmt.Println(err)
			// 	}
			// } else if id == 8 {
			// 	f.streamQueue = append(f.streamQueue, id)
			}
		} else { // no more data to send. Stream is not active any more
			delete(f.activeStreams, id)
			// fmt.Println("No mas datos")
		}

		fmt.Println("Cola Stream Despues Prioridad:  ",f.streamQueue)
		lenQueue := int(len(f.streamQueue)) 
		bytesBufferperStream := make([]protocol.ByteCount, lenQueue)
		idBuffer := make([]protocol.StreamID, lenQueue)
				
		for k := 0; k < lenQueue; k++ {
			id = f.streamQueue[k] 
			str,_ := f.streamGetter.GetOrOpenSendStream(id)
			bufferTx := str.RemainingBytes() 
			bufferRtx := str.BytesToRetransmit()
			bufferTotal := (bufferTx + bufferRtx)
			fmt.Println("ID: ", id ,"   NumB cola TX: ", bufferTx, "   NumB cola RTX: ", bufferRtx, "   NumB Total: ", bufferTotal)
			// if bufferTotal == 0 {
			// 	continue
			// }
			idBuffer[k] = id
			bytesBufferperStream[k] =  bufferTotal
		}
		//Comparacion de los streams
		f.streamQueue = nil
		for k := 0; k < lenQueue; k++ {
			firstID := maxIndex(bytesBufferperStream)
			idReal := idBuffer[firstID]
			// f.streamQueue, _ = insert(f.streamQueue, k, idReal)
			f.streamQueue = append(f.streamQueue, idReal)
			bytesBufferperStream[firstID] = -10
		}
		fmt.Println("Cola Stream Despues Buffer:  ", f.streamQueue)
		
		// The frame can be nil
		// * if the receiveStream was canceled after it said it had data
		// * the remaining size doesn't allow us to add another STREAM frame
		if frame == nil {
			continue
		}
		frames = append(frames, *frame)
		length += frame.Length(f.version)
		lastFrame = frame
	}
	
	f.mutex.Unlock()
	if lastFrame != nil {
		lastFrameLen := lastFrame.Length(f.version)
		// account for the smaller size of the last STREAM frame
		lastFrame.Frame.(*wire.StreamFrame).DataLenPresent = false
		length += lastFrame.Length(f.version) - lastFrameLen
	}
	return frames, length
}
*/
