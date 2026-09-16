//go:build linux && cgo
// +build linux,cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef int32_t cl_int;
typedef uint32_t cl_uint;
typedef uint64_t cl_ulong;
typedef cl_ulong cl_bitfield;
typedef cl_bitfield cl_device_type;
typedef cl_bitfield cl_mem_flags;
typedef intptr_t cl_context_properties;
typedef void* cl_platform_id;
typedef void* cl_device_id;
typedef void* cl_context;
typedef void* cl_command_queue;
typedef void* cl_mem;
typedef void* cl_program;
typedef void* cl_kernel;
typedef uint32_t cl_bool;

#define CL_SUCCESS 0
#define CL_DEVICE_TYPE_GPU ((cl_device_type)(1ULL << 2))
#define CL_MEM_WRITE_ONLY ((cl_mem_flags)(1ULL << 1))
#define CL_MEM_READ_ONLY ((cl_mem_flags)(1ULL << 2))
#define CL_TRUE 1

typedef cl_int (*p_clGetPlatformIDs)(cl_uint, cl_platform_id*, cl_uint*);
typedef cl_int (*p_clGetDeviceIDs)(cl_platform_id, cl_device_type, cl_uint, cl_device_id*, cl_uint*);
typedef cl_context (*p_clCreateContext)(const cl_context_properties*, cl_uint, const cl_device_id*, void*, void*, cl_int*);
typedef cl_command_queue (*p_clCreateCommandQueue)(cl_context, cl_device_id, cl_bitfield, cl_int*);
typedef cl_program (*p_clCreateProgramWithSource)(cl_context, cl_uint, const char**, const size_t*, cl_int*);
typedef cl_int (*p_clBuildProgram)(cl_program, cl_uint, const cl_device_id*, const char*, void*, void*);
typedef cl_kernel (*p_clCreateKernel)(cl_program, const char*, cl_int*);
typedef cl_mem (*p_clCreateBuffer)(cl_context, cl_mem_flags, size_t, void*, cl_int*);
typedef cl_int (*p_clSetKernelArg)(cl_kernel, cl_uint, size_t, const void*);
typedef cl_int (*p_clEnqueueWriteBuffer)(cl_command_queue, cl_mem, cl_bool, size_t, size_t, const void*, cl_uint, const void*, void*);
typedef cl_int (*p_clEnqueueNDRangeKernel)(cl_command_queue, cl_kernel, cl_uint, const size_t*, const size_t*, const size_t*, cl_uint, const void*, void*);
typedef cl_int (*p_clEnqueueReadBuffer)(cl_command_queue, cl_mem, cl_bool, size_t, size_t, void*, cl_uint, const void*, void*);
typedef cl_int (*p_clFinish)(cl_command_queue);
typedef cl_int (*p_clReleaseMemObject)(cl_mem);
typedef cl_int (*p_clReleaseKernel)(cl_kernel);
typedef cl_int (*p_clReleaseProgram)(cl_program);
typedef cl_int (*p_clReleaseCommandQueue)(cl_command_queue);
typedef cl_int (*p_clReleaseContext)(cl_context);

typedef struct {
  void* lib;
  p_clGetPlatformIDs clGetPlatformIDs;
  p_clGetDeviceIDs clGetDeviceIDs;
  p_clCreateContext clCreateContext;
  p_clCreateCommandQueue clCreateCommandQueue;
  p_clCreateProgramWithSource clCreateProgramWithSource;
  p_clBuildProgram clBuildProgram;
  p_clCreateKernel clCreateKernel;
  p_clCreateBuffer clCreateBuffer;
  p_clSetKernelArg clSetKernelArg;
  p_clEnqueueWriteBuffer clEnqueueWriteBuffer;
  p_clEnqueueNDRangeKernel clEnqueueNDRangeKernel;
  p_clEnqueueReadBuffer clEnqueueReadBuffer;
  p_clFinish clFinish;
  p_clReleaseMemObject clReleaseMemObject;
  p_clReleaseKernel clReleaseKernel;
  p_clReleaseProgram clReleaseProgram;
  p_clReleaseCommandQueue clReleaseCommandQueue;
  p_clReleaseContext clReleaseContext;
} memai_cl_api;

static int memai_cl_load(memai_cl_api* a) {
  memset(a, 0, sizeof(*a));
  const char* names[] = {"libOpenCL.so.1", "libOpenCL.so", NULL};
  for (int i = 0; names[i] != NULL && a->lib == NULL; ++i) {
    a->lib = dlopen(names[i], RTLD_NOW | RTLD_LOCAL);
  }
  if (!a->lib) return -1;
#define MEMAI_LOAD(name) do { a->name = (p_##name)dlsym(a->lib, #name); if (!a->name) { dlclose(a->lib); a->lib = NULL; return -2; } } while (0)
  MEMAI_LOAD(clGetPlatformIDs);
  MEMAI_LOAD(clGetDeviceIDs);
  MEMAI_LOAD(clCreateContext);
  MEMAI_LOAD(clCreateCommandQueue);
  MEMAI_LOAD(clCreateProgramWithSource);
  MEMAI_LOAD(clBuildProgram);
  MEMAI_LOAD(clCreateKernel);
  MEMAI_LOAD(clCreateBuffer);
  MEMAI_LOAD(clSetKernelArg);
  MEMAI_LOAD(clEnqueueWriteBuffer);
  MEMAI_LOAD(clEnqueueNDRangeKernel);
  MEMAI_LOAD(clEnqueueReadBuffer);
  MEMAI_LOAD(clFinish);
  MEMAI_LOAD(clReleaseMemObject);
  MEMAI_LOAD(clReleaseKernel);
  MEMAI_LOAD(clReleaseProgram);
  MEMAI_LOAD(clReleaseCommandQueue);
  MEMAI_LOAD(clReleaseContext);
#undef MEMAI_LOAD
  return 0;
}

static void memai_cl_unload(memai_cl_api* a) {
  if (a && a->lib) dlclose(a->lib);
  if (a) memset(a, 0, sizeof(*a));
}

static int memai_cl_first_gpu(memai_cl_api* a, cl_device_id* device_out) {
  cl_uint platform_count = 0;
  if (a->clGetPlatformIDs(0, NULL, &platform_count) != CL_SUCCESS || platform_count == 0) return -3;
  cl_platform_id* platforms = (cl_platform_id*)calloc(platform_count, sizeof(cl_platform_id));
  if (!platforms) return -4;
  if (a->clGetPlatformIDs(platform_count, platforms, NULL) != CL_SUCCESS) { free(platforms); return -5; }
  int rc = -6;
  for (cl_uint i = 0; i < platform_count; ++i) {
    cl_uint device_count = 0;
    cl_int er = a->clGetDeviceIDs(platforms[i], CL_DEVICE_TYPE_GPU, 0, NULL, &device_count);
    if (er != CL_SUCCESS || device_count == 0) continue;
    cl_device_id device = NULL;
    er = a->clGetDeviceIDs(platforms[i], CL_DEVICE_TYPE_GPU, 1, &device, NULL);
    if (er == CL_SUCCESS && device) { *device_out = device; rc = 0; break; }
  }
  free(platforms);
  return rc;
}

static int memai_opencl_probe(void) {
  memai_cl_api a;
  int rc = memai_cl_load(&a);
  if (rc != 0) return rc;
  cl_device_id device = NULL;
  rc = memai_cl_first_gpu(&a, &device);
  memai_cl_unload(&a);
  return rc;
}

static int memai_opencl_dot(const float* vectors, const float* weights, float* out, uint32_t rows, uint32_t lanes) {
  if (!vectors || !weights || !out || rows == 0 || lanes == 0) return -10;
  memai_cl_api a;
  int rc = memai_cl_load(&a);
  if (rc != 0) return rc;
  cl_device_id device = NULL;
  if ((rc = memai_cl_first_gpu(&a, &device)) != 0) { memai_cl_unload(&a); return rc; }

  cl_int er = CL_SUCCESS;
  cl_context ctx = NULL;
  cl_command_queue queue = NULL;
  cl_program program = NULL;
  cl_kernel kernel = NULL;
  cl_mem vb = NULL, wb = NULL, ob = NULL;
  const char* source =
    "__kernel void memai_dot(__global const float* v,__global const float* w,__global float* o,uint lanes){"
    "size_t r=get_global_id(0);size_t b=r*(size_t)lanes;float s=0.0f;"
    "for(uint i=0;i<lanes;i++){s+=v[b+i]*w[i];}o[r]=s;}";

  ctx = a.clCreateContext(NULL, 1, &device, NULL, NULL, &er);
  if (!ctx || er != CL_SUCCESS) { rc = -20; goto done; }
  queue = a.clCreateCommandQueue(ctx, device, 0, &er);
  if (!queue || er != CL_SUCCESS) { rc = -21; goto done; }
  program = a.clCreateProgramWithSource(ctx, 1, &source, NULL, &er);
  if (!program || er != CL_SUCCESS) { rc = -22; goto done; }
  er = a.clBuildProgram(program, 1, &device, NULL, NULL, NULL);
  if (er != CL_SUCCESS) { rc = -23; goto done; }
  kernel = a.clCreateKernel(program, "memai_dot", &er);
  if (!kernel || er != CL_SUCCESS) { rc = -24; goto done; }

  size_t vector_bytes = (size_t)rows * (size_t)lanes * sizeof(float);
  size_t weight_bytes = (size_t)lanes * sizeof(float);
  size_t out_bytes = (size_t)rows * sizeof(float);
  vb = a.clCreateBuffer(ctx, CL_MEM_READ_ONLY, vector_bytes, NULL, &er);
  if (!vb || er != CL_SUCCESS) { rc = -25; goto done; }
  wb = a.clCreateBuffer(ctx, CL_MEM_READ_ONLY, weight_bytes, NULL, &er);
  if (!wb || er != CL_SUCCESS) { rc = -26; goto done; }
  ob = a.clCreateBuffer(ctx, CL_MEM_WRITE_ONLY, out_bytes, NULL, &er);
  if (!ob || er != CL_SUCCESS) { rc = -27; goto done; }

  if (a.clEnqueueWriteBuffer(queue, vb, CL_TRUE, 0, vector_bytes, vectors, 0, NULL, NULL) != CL_SUCCESS) { rc = -28; goto done; }
  if (a.clEnqueueWriteBuffer(queue, wb, CL_TRUE, 0, weight_bytes, weights, 0, NULL, NULL) != CL_SUCCESS) { rc = -29; goto done; }
  if (a.clSetKernelArg(kernel, 0, sizeof(cl_mem), &vb) != CL_SUCCESS ||
      a.clSetKernelArg(kernel, 1, sizeof(cl_mem), &wb) != CL_SUCCESS ||
      a.clSetKernelArg(kernel, 2, sizeof(cl_mem), &ob) != CL_SUCCESS ||
      a.clSetKernelArg(kernel, 3, sizeof(cl_uint), &lanes) != CL_SUCCESS) { rc = -30; goto done; }
  size_t global = (size_t)rows;
  if (a.clEnqueueNDRangeKernel(queue, kernel, 1, NULL, &global, NULL, 0, NULL, NULL) != CL_SUCCESS) { rc = -31; goto done; }
  if (a.clFinish(queue) != CL_SUCCESS) { rc = -32; goto done; }
  if (a.clEnqueueReadBuffer(queue, ob, CL_TRUE, 0, out_bytes, out, 0, NULL, NULL) != CL_SUCCESS) { rc = -33; goto done; }
  rc = 0;

done:
  if (ob) a.clReleaseMemObject(ob);
  if (wb) a.clReleaseMemObject(wb);
  if (vb) a.clReleaseMemObject(vb);
  if (kernel) a.clReleaseKernel(kernel);
  if (program) a.clReleaseProgram(program);
  if (queue) a.clReleaseCommandQueue(queue);
  if (ctx) a.clReleaseContext(ctx);
  memai_cl_unload(&a);
  return rc;
}
*/
import "C"

import (
	"fmt"
	"sync/atomic"
	"unsafe"
)

type openCLPhysicalGPU struct {
	available bool
	probes    uint64
	calls     uint64
	failures  uint64
}

func newPhysicalGPUBackend() physicalGPUBackend {
	g := &openCLPhysicalGPU{}
	atomic.AddUint64(&g.probes, 1)
	g.available = C.memai_opencl_probe() == 0
	return g
}

func (g *openCLPhysicalGPU) Name() string { return "opencl" }
func (g *openCLPhysicalGPU) Available() bool {
	return g != nil && g.available
}

func flattenPhysicalVectors(vectors [][]float32, lanes int) []float32 {
	flat := make([]float32, len(vectors)*lanes)
	for row := range vectors {
		copy(flat[row*lanes:(row+1)*lanes], vectors[row])
	}
	return flat
}

func (g *openCLPhysicalGPU) Dot(vectors [][]float32, weights []float32) ([]float32, error) {
	if g == nil || !g.available {
		return nil, errPhysicalGPUUnavailable
	}
	if err := validateDotShape(vectors, weights); err != nil {
		return nil, err
	}
	if len(vectors) == 0 {
		return []float32{}, nil
	}
	atomic.AddUint64(&g.calls, 1)
	flat := flattenPhysicalVectors(vectors, len(weights))
	out := make([]float32, len(vectors))
	rc := C.memai_opencl_dot(
		(*C.float)(unsafe.Pointer(&flat[0])),
		(*C.float)(unsafe.Pointer(&weights[0])),
		(*C.float)(unsafe.Pointer(&out[0])),
		C.uint32_t(len(vectors)),
		C.uint32_t(len(weights)),
	)
	if rc != 0 {
		atomic.AddUint64(&g.failures, 1)
		return nil, fmt.Errorf("OpenCL physical dot failed: code=%d", int(rc))
	}
	return out, nil
}

func (g *openCLPhysicalGPU) Info() map[string]any {
	available := false
	if g != nil {
		available = g.available
	}
	return map[string]any{
		"available": available,
		"backend":   "opencl-dynamic",
		"semantic":  false,
		"probes":    atomic.LoadUint64(&g.probes),
		"calls":     atomic.LoadUint64(&g.calls),
		"failures":  atomic.LoadUint64(&g.failures),
	}
}
