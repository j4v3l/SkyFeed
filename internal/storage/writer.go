package storage

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
)

var ErrWriterFull = errors.New("persistence queue full")

type WriterStats struct {
	Accepted uint64
	Dropped  uint64
	Batches  uint64
	Written  uint64
	Failed   uint64
	LastSize int
	Latency  time.Duration
}

type Writer struct {
	sink     BatchSink
	queue    chan WriteEvent
	batchMax int
	flush    time.Duration

	accepted atomic.Uint64
	dropped  atomic.Uint64
	batches  atomic.Uint64
	written  atomic.Uint64
	failed   atomic.Uint64
	lastSize atomic.Int64
	latency  atomic.Int64
}

func NewWriter(sink BatchSink, capacity, batchMax int, flush time.Duration) *Writer {
	return &Writer{sink: sink, queue: make(chan WriteEvent, capacity), batchMax: batchMax, flush: flush}
}

func (writer *Writer) Enqueue(event WriteEvent) error {
	select {
	case writer.queue <- event:
		writer.accepted.Add(1)
		return nil
	default:
		writer.dropped.Add(1)
		return ErrWriterFull
	}
}

func (writer *Writer) Run(ctx context.Context) error {
	ticker := time.NewTicker(writer.flush)
	defer ticker.Stop()
	batch := make([]WriteEvent, 0, writer.batchMax)
	for {
		select {
		case <-ctx.Done():
			flushContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			for {
				writer.drain(&batch)
				if len(batch) == 0 {
					return nil
				}
				if err := writer.apply(flushContext, batch); err != nil {
					return err
				}
				batch = batch[:0]
			}
		case event := <-writer.queue:
			batch = append(batch, event)
			if len(batch) >= writer.batchMax {
				if err := writer.apply(ctx, batch); err != nil {
					return err
				}
				batch = batch[:0]
			}
		case <-ticker.C:
			if err := writer.apply(ctx, batch); err != nil {
				return err
			}
			batch = batch[:0]
		}
	}
}

func (writer *Writer) Stats() WriterStats {
	return WriterStats{Accepted: writer.accepted.Load(), Dropped: writer.dropped.Load(), Batches: writer.batches.Load(), Written: writer.written.Load(), Failed: writer.failed.Load(), LastSize: int(writer.lastSize.Load()), Latency: time.Duration(writer.latency.Load())}
}

func (writer *Writer) Depth() int { return len(writer.queue) }

func (writer *Writer) drain(batch *[]WriteEvent) {
	for len(*batch) < writer.batchMax {
		select {
		case event := <-writer.queue:
			*batch = append(*batch, event)
		default:
			return
		}
	}
}

func (writer *Writer) apply(ctx context.Context, batch []WriteEvent) error {
	if len(batch) == 0 {
		return nil
	}
	writer.batches.Add(1)
	started := time.Now()
	writer.lastSize.Store(int64(len(batch)))
	if err := writer.sink.ApplyBatch(ctx, batch); err != nil {
		writer.latency.Store(time.Since(started).Nanoseconds())
		writer.failed.Add(uint64(len(batch)))
		return err
	}
	writer.latency.Store(time.Since(started).Nanoseconds())
	writer.written.Add(uint64(len(batch)))
	return nil
}
