// Package mapreduce provides a simple mapreduce library with a sequential
// implementation. Applications should normally call Distributed() [located in
// master.go] to start a job, but may instead call Sequential() [also in
// master.go] to get a sequential execution for debugging purposes.
//
// The flow of the mapreduce implementation is as follows:
//
//   1. The application provides a number of input files, a map function, a
//      reduce function, and the number of reduce tasks (nReduce).
//   2. A master is created with this knowledge. It spins up an RPC server (see
//      master_rpc.go), and waits for workers to register (using the RPC call
//      Register() [defined in master.go]). As tasks become available (in steps
//      4 and 5), schedule() [schedule.go] decides how to assign those tasks to
//      workers, and how to handle worker failures.
//   3. The master considers each input file one map tasks, and makes a call to
//      doMap() [common_map.go] at least once for each task. It does so either
//      directly (when using Sequential()) or by issuing the DoJob RPC on a
//      worker [worker.go]. Each call to doMap() reads the appropriate file,
//      calls the map function on that file's contents, and produces nReduce
//      files for each map file. Thus, there will be #files x nReduce files
//      after all map tasks are done:
//
//          f0-0, ..., f0-0, f0-<nReduce-1>, ...,
//          f<#files-1>-0, ... f<#files-1>-<nReduce-1>.
//
//   4. The master next makes a call to doReduce() [common_reduce.go] at least
//      once for each reduce task. As for doMap(), it does so either directly or
//      through a worker. doReduce() collects nReduce reduce files from each
//      map (f-*-<reduce>), and runs the reduce function on those files. This
//      produces nReduce result files.
//   5. The master calls mr.merge() [master_splitmerge.go], which merges all
//      the nReduce files produced by the previous step into a single output.
//   6. The master sends a Shutdown RPC to each of its workers, and then shuts
//      down its own RPC server.
//
// TODO:
// You will have to write/modify doMap, doReduce, and schedule yourself. These
// are located in common_map.go, common_reduce.go, and schedule.go
// respectively. You will also have to write the map and reduce functions in
// ../main/wc.go.
//
// You should not need to modify any other files, but reading them might be
// useful in order to understand how the other methods fit into the overall
// architecture of the system.
package mapreduce
