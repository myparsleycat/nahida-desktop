// src/core/lib/zstd/index.ts

// @ts-ignore
import wasmUrl from './zstd_wasm_bg.wasm?url';

export interface WasmExports {
  memory: WebAssembly.Memory;
  __wbindgen_malloc: (size: number, align: number) => number;
  __wbindgen_free: (ptr: number, size: number, align: number) => void;
  __wbindgen_add_to_stack_pointer: (n: number) => number;
  compress: (retptr: number, ptr: number, len: number, level: number) => void;
  decompress: (retptr: number, ptr: number, len: number) => void;
}

export default class ZstdWasm {
  private static instance: ZstdWasm | null = null;
  private wasm: WasmExports;
  private cachedUint8ArrayMemory0: Uint8Array | null = null;
  private cachedDataViewMemory0: DataView | null = null;
  private WASM_VECTOR_LEN: number = 0;

  private constructor(wasmExports: WasmExports) {
    this.wasm = wasmExports;
  }

  public static async getInstance(): Promise<ZstdWasm> {
    if (!ZstdWasm.instance) {
      let wasmExports: WasmExports;
      const response = await fetch(new URL(wasmUrl, import.meta.url).href);
      let wasmModule: WebAssembly.WebAssemblyInstantiatedSource;
      try {
        wasmModule = await WebAssembly.instantiateStreaming(response, {});
      } catch (e) {
        const bytes = await response.arrayBuffer();
        wasmModule = await WebAssembly.instantiate(bytes, {});
      }
      wasmExports = wasmModule.instance.exports as unknown as WasmExports;
      ZstdWasm.instance = new ZstdWasm(wasmExports);
    }
    return ZstdWasm.instance;
  }

  public static async init(): Promise<void> {
    await ZstdWasm.getInstance();
  }

  // wasm 메모리를 Uint8Array로 캐싱
  private getUint8ArrayMemory0(): Uint8Array {
    if (this.cachedUint8ArrayMemory0 === null || this.cachedUint8ArrayMemory0.byteLength === 0) {
      this.cachedUint8ArrayMemory0 = new Uint8Array(this.wasm.memory.buffer);
    }
    return this.cachedUint8ArrayMemory0;
  }

  // wasm 메모리를 DataView로 캐싱
  private getDataViewMemory0(): DataView {
    if (
      this.cachedDataViewMemory0 === null ||
      this.cachedDataViewMemory0.buffer !== this.wasm.memory.buffer ||
      (this.cachedDataViewMemory0.buffer as any).detached === true
    ) {
      this.cachedDataViewMemory0 = new DataView(this.wasm.memory.buffer);
    }
    return this.cachedDataViewMemory0;
  }

  // Uint8Array를 wasm 메모리로 복사하고 포인터를 리턴
  private passArray8ToWasm0(arg: Uint8Array, malloc: (size: number, align: number) => number): number {
    const ptr = malloc(arg.length, 1) >>> 0;
    this.getUint8ArrayMemory0().set(arg, ptr);
    this.WASM_VECTOR_LEN = arg.length;
    return ptr;
  }

  public compress(source: Uint8Array, level: number = 3): Uint8Array {
    const retptr = this.wasm.__wbindgen_add_to_stack_pointer(-16);
    try {
      const ptr0 = this.passArray8ToWasm0(source, this.wasm.__wbindgen_malloc);
      const len0 = this.WASM_VECTOR_LEN;
      this.wasm.compress(retptr, ptr0, len0, level);
      const mem = this.getDataViewMemory0();
      const r0 = mem.getInt32(retptr, true);
      const r1 = mem.getInt32(retptr + 4, true);
      const result = this.getUint8ArrayMemory0().subarray(r0, r0 + r1).slice();
      this.wasm.__wbindgen_free(r0, r1, 1);
      return result;
    } finally {
      this.wasm.__wbindgen_add_to_stack_pointer(16);
    }
  }

  public decompress(source: Uint8Array): Uint8Array {
    const retptr = this.wasm.__wbindgen_add_to_stack_pointer(-16);
    try {
      const ptr0 = this.passArray8ToWasm0(source, this.wasm.__wbindgen_malloc);
      const len0 = this.WASM_VECTOR_LEN;
      this.wasm.decompress(retptr, ptr0, len0);
      const mem = this.getDataViewMemory0();
      const r0 = mem.getInt32(retptr, true);
      const r1 = mem.getInt32(retptr + 4, true);
      const result = this.getUint8ArrayMemory0().subarray(r0, r0 + r1).slice();
      this.wasm.__wbindgen_free(r0, r1, 1);
      return result;
    } finally {
      this.wasm.__wbindgen_add_to_stack_pointer(16);
    }
  }
}
