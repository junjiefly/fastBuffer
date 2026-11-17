[English](README.md) | [简体中文](README.zh-CN.md)

# fast buffer：Go 高性能字节缓冲池

* 普通 bytes buffer 在读写流式数据时需要增长内存，容易产生内存碎片，也会增加 GC 压力。
* 实际上很多场景可以提前知道读写大小，例如 HTTP 的 content-length，或者文件按 `read(fd, offset, length)` 读取时的长度。

* Summary
* 如果知道读写大小，使用 fast buffer。
* 如果不知道精确大小，但能估算最大大小，也可以使用 fast buffer。
* fast buffer 有内存池，反复分配和释放时可以复用内存，更大的预分配块通常不会带来严重问题。
* 如果无法估算最大大小，使用标准库 bytes buffer。

### Features
* 内存池
* reader / writer
* DIO 对齐缓冲区
* 支持包装外部内存，例如 RDMA 注册内存

### DIO 模式

DIO 模式用于配合 direct I/O 标志，例如 Linux 的 `O_DIRECT` 和 Windows 的
`FILE_FLAG_NO_BUFFERING`。

#### 分配 DIO 缓冲区

```go
size := 4 * fastBuffer.MBSize
fb, err := fastBuffer.NewDioFB(size, 4096)
if err != nil {
    return err
}

// fb.GetBuf() 的起始地址和长度都按 4096 对齐。
f, err := os.OpenFile(path, os.O_RDWR|syscall.O_DIRECT, 0600)
if err != nil {
    fastBuffer.FreeFB(fb)
    return err
}
_, err = f.WriteAt(fb.GetBuf(), 0)

f.Close()
fastBuffer.FreeFB(fb)
```

`NewDioFB(size, align)` 要求 `align` 是 2 的幂。`size` 会向上取整到 `align`，
因此刚分配后的 `GetBuf()` 起始地址和长度都是对齐的。例如 `NewDioFB(4MB, 4096)`
返回 4MB 缓冲区；`NewDioFB(5000, 4096)` 返回 8192 字节缓冲区。

DIO 使用独立的精确大小内存池。底层 slab 大小是 `roundedSize + align`。这样
`4MB + 4KB` 不会被普通 power-of-two 内存池提升到 8MB，避免常规卷场景内存翻倍。

分配出来的 DIO 缓冲区必须使用 `FreeFB(fb)` 释放。不要把 `fb.GetBuf()` 传给
`Free()`；它只是更大 DIO slab 里的一个对齐视图，直接 `Free` 可能破坏内存池
所有权。

#### 外部内存和 RDMA

如果内存由外部组件持有，例如已经注册的 RDMA buffer，使用 `WrapDio` 包装：

```go
// data 的起始地址和长度都必须按 align 对齐。
fb, err := fastBuffer.WrapDio(rdmaBuffer, 4096)
if err != nil {
    return err
}

// ... 执行 I/O ...
fastBuffer.FreeFB(fb)
// rdmaBuffer 仍然由 RDMA 组件持有和释放。
```

`WrapDio` 只校验起始地址和长度，不会复制内存，也不会自动注册 RDMA MR。
调用方必须保证内存有效，并且在所有 in-flight I/O 完成前不能释放或改写它。

`NewDioFB` 的内存由 Go 管理，适合 direct file I/O。它不会自动做 RDMA 注册。
使用 RDMA 时，内存注册和 MR/LKey/RKey 的所有权应保留在 RDMA 组件中，然后用
`WrapDio` 包装已注册内存。

#### 对齐规则

direct I/O 操作通常要求内存起始地址、内存长度和文件偏移满足设备/文件系统的
对齐要求。`NewDioFB` 和 `WrapDio` 保证缓冲区起始地址以及初始缓冲区长度对齐；
调用方仍需要保证实际传入 syscall 的 slice 偏移/长度和文件偏移对齐。

`Reset`、`ReSize`、`Read`、`ReadFrom`、`WriteTo` 等方法可能改变有效偏移或
长度。不要把未对齐的 view 传给 direct I/O syscall。不确定时，直接使用对齐的
`GetBuf()`，或者确保传入 syscall 的 slice 起始偏移和长度都按 `align` 对齐。

macOS 没有真正的 `O_DIRECT`。如果需要关闭缓存，通常配合 `fcntl(F_NOCACHE)`
使用；它不会像 `O_DIRECT` 一样强制执行对齐检查。
