# fast buffer -- fast bytes buffer with mem pool in GO

* bytes buffer provides a tool to r/w data stream, but it has to call "grow" as the data size increases, which 
* produces mem fragment, but gc hates it.
* in fact, most of the time, we can get the data size before we read from a stream. 
* For example, http has the content-length header,
* and data size on disk because you have to call the read(fd,offset,length). 
  
* Summary
* If you know the r/w data size, use fast buffer with no doubt.
* 
* If you do not know the data size, but you can estimate the max data size, use fast buffer with no doubt.
* (in this scenario, allocates a bigger mem block for fast buffer. fast buffer has mem pools, reuse happens oftenly, so bigger mem block does not matter very likely)
* 
* If you do not know the data size, and you can not estimate the max data size, then you should use bytes buffer.

### Features
* contains mem pools
* contains reader and writer