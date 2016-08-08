package mapreduce

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
)

// merge combines the results of the many reduce jobs into a single output file
// XXX use merge sort
// 将reduce工作产生的结果合并成一个输出文件。
func (mr *Master) merge() {
	debug("Merge phase")
	kvs := make(map[string]string)
	// nReduce个reduce任务，有nReduce个任务。
	for i := 0; i < mr.nReduce; i++ {
		p := mergeName(mr.jobName, i) 	// 获取输出文件，类似mrtmp.test-res-0
		fmt.Printf("Merge: read %s\n", p)
		file, err := os.Open(p)
		if err != nil {
			log.Fatal("Merge: ", err)
		}
		dec := json.NewDecoder(file) // json数据的流式读写
		for {
			var kv KeyValue
			err = dec.Decode(&kv)
			if err != nil {
				break
			}
			kvs[kv.Key] = kv.Value
		}
		file.Close()
	}

	// key排序
	var keys []string
	for k := range kvs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 将排序后的结果写入文件mrtmp.*
	file, err := os.Create("mrtmp." + mr.jobName)
	if err != nil {
		log.Fatal("Merge: create ", err)
	}
	w := bufio.NewWriter(file)
	for _, k := range keys {
		fmt.Fprintf(w, "%s: %s\n", k, kvs[k])
	}
	w.Flush()
	file.Close()
}

// removeFile is a simple wrapper around os.Remove that logs errors.
func removeFile(n string) {
	err := os.Remove(n)
	if err != nil {
		log.Fatal("CleanupFiles ", err)
	}
}

// CleanupFiles removes all intermediate files produced by running mapreduce.
// 删除mapreduce删除的全部中间文件。
func (mr *Master) CleanupFiles() {
	for i := range mr.files {
		for j := 0; j < mr.nReduce; j++ {
			removeFile(reduceName(mr.jobName, i, j))
		}
	}
	for i := 0; i < mr.nReduce; i++ {
		removeFile(mergeName(mr.jobName, i))
	}
	removeFile("mrtmp." + mr.jobName)
}
