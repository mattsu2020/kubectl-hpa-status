package hpa

import "time"

// now is a package-level indirection over time.Now so that snapshot tests
// can freeze the clock by reassigning this variable. It must not be changed
// concurrently with normal package use; tests reassign it before running.
var now = time.Now
