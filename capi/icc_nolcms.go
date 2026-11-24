//go:build nolcms2

package main

import "unsafe"

const colorManagementBuilt = false

func iccCreate([]byte, bool) unsafe.Pointer                  { return nil }
func iccApply(unsafe.Pointer, unsafe.Pointer, int, int, int) {}
func iccFree(unsafe.Pointer)                                 {}
