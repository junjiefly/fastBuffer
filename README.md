[English](README.md) | [简体中文](README.zh-CN.md)

# fast buffer -- fast bytes buffer with mem pool in GO

* bytes buffer provides a tool to r/w data stream, but it has to call "grow" as the data size increases, which
* produces mem fragment, but gc hates it.
* in fact, most of the time, we can get the data size before we read from a stream.
* For example, http has the content-length header,
* and data size on disk because you have to call the read(fd,offset,length).

* Summary
* If you know the r/w data size, use fast buffer.
*
* If you do not know the data size, but you can estimate the max data size, use fast buffer.
* (in this scenario, allocates a bigger mem block for fast buffer. fast buffer has mem pools, reuse happens oftenly, so bigger mem block does not matter very likely)
*
* If you do not know the data size, and you can not estimate the max data size, then you should use bytes buffer.

### Features
* contains mem pools
* contains reader and writer

### DIO mode

DIO mode helps callers use direct I/O flags such as Linux `O_DIRECT` and
Windows `FILE_FLAG_NO_BUFFERING`.

#### Allocated DIO buffers

```go
size := 4 * fastBuffer.MBSize
fb, err := fastBuffer.NewDioFB(size, 4096)
if err != nil {
    return err
}

// fb.GetBuf() has a 4096-aligned base and a 4096-aligned length.
f, err := os.OpenFile(path, os.O_RDWR|syscall.O_DIRECT, 0600)
if err != nil {
    fastBuffer.FreeFB(fb)
    return err
}
_, err = f.WriteAt(fb.GetBuf(), 0)

f.Close()
fastBuffer.FreeFB(fb)
```

`NewDioFB(size, align)` requires `align` to be a power of two. The requested
size is rounded up to `align`, so `GetBuf()` initially returns a slice whose
base address and length are aligned. For example, `NewDioFB(4MB, 4096)` returns
a 4MB buffer; `NewDioFB(5000, 4096)` returns an 8192-byte buffer.

DIO allocations use a dedicated exact-size pool. The underlying slab is
`roundedSize + align` bytes. This avoids promoting `4MB + 4KB` to the next
power-of-two normal pool and allocating an 8MB slab.

Always release an allocated DIO buffer with `FreeFB(fb)`. Never pass
`fb.GetBuf()` to `Free()`; it is an aligned view into a larger DIO slab, and
freeing it directly can corrupt pool ownership.

#### External and RDMA memory

Use `WrapDio(data, align)` when the memory is owned elsewhere, for example an
RDMA-registered buffer:

```go
// data must be aligned and its length must be a multiple of align.
fb, err := fastBuffer.WrapDio(rdmaBuffer, 4096)
if err != nil {
    return err
}

// ... perform I/O ...
fastBuffer.FreeFB(fb)
// rdmaBuffer is still owned and released by the RDMA component.
```

`WrapDio` validates the base address and length but does not copy or register
the memory. The owner must keep the memory alive and unchanged until all
in-flight I/O completes.

`NewDioFB` memory is Go-managed and suitable for direct file I/O. It is not
RDMA-registered automatically. For RDMA, keep memory registration and MR/LKey/
RKey ownership in the RDMA component and wrap the registered memory with
`WrapDio`.

#### Alignment rules

For a direct I/O operation, the memory base, memory length, and file offset
normally must satisfy the device and filesystem alignment requirements.
`NewDioFB` and `WrapDio` align the buffer base and the initial buffer length.
The caller must still keep the active slice offset/length and file offset
aligned.

Methods such as `Reset`, `ReSize`, `Read`, `ReadFrom`, and `WriteTo` can change
the active offset or length. Do not pass an unaligned view to a direct I/O
syscall. When in doubt, use the aligned `GetBuf()` slice directly, or ensure
that the slice passed to the syscall starts at an aligned offset and has an
aligned length.
