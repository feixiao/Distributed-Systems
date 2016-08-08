package mapreduce

import (
	"hash/fnv"
	"io/ioutil"
	"os"
	"encoding/json"
	"fmt"
)

// doMap does the job of a map worker: it reads one of the input files
// (inFile), calls the user-defined map function (mapF) for that file's
// contents, and partitions the output into nReduce intermediate files.
// doMap的工作内容如下：读取输入文件（inFile）, 调用用户自己定义的map函数mapF处理文件内容，
// 分割输出到nReduce份中间文件。
func doMap(
	jobName string, // the name of the MapReduce job
	mapTaskNumber int, // which map task this is
	inFile string,
	nReduce int, // the number of reduce task that will be run ("R" in the paper)
	mapF func(file string, contents string) []KeyValue,
) {
	// TODO:
	// You will need to write this function.
	// You can find the filename for this map task's input to reduce task number
	// r using reduceName(jobName, mapTaskNumber, r). The ihash function (given
	// below doMap) should be used to decide which file a given key belongs into.
	//
	// The intermediate output of a map task is stored in the file
	// system as multiple files whose name indicates which map task produced
	// them, as well as which reduce task they are for. Coming up with a
	// scheme for how to store the key/value pairs on disk can be tricky,
	// especially when taking into account that both keys and values could
	// contain newlines, quotes, and any other character you can think of.
	//
	// One format often used for serializing data to a byte stream that the
	// other end can correctly reconstruct is JSON. You are not required to
	// use JSON, but as the output of the reduce tasks *must* be JSON,
	// familiarizing yourself with it here may prove useful. You can write
	// out a data structure as a JSON string to a file using the commented
	// code below. The corresponding decoding functions can be found in
	// common_reduce.go.
	//
	//   enc := json.NewEncoder(file)
	//   for _, kv := ... {
	//     err := enc.Encode(&kv)
	//
	// Remember to close the file after you have written all the values!

	// 你需要重写这个函数。你可以通过reduceName获取文件名,使用map任务的输入为reduce任务提供输出。
	// 下面给出的ihash函数应该被用于决定每个key属于的文件。
	//
	// map任务的中间输入以多文件的形式保存在文件系统上,它们的文件名说明是哪个map任务产生的,同时也说明哪个reduce任务会处理它们。
	// 想出如何存储键/值对在磁盘上的方案可能会非常棘手,特别地, 当我们考虑到key和value都包含新行(newlines),引用(quotes),或者其他
	// 你想到的字符。
	//
	// 有一种格式经常被用来序列化数据到字节流,然后可以通过字节流进行重建，这种格式是json。你没有被强制使用JSON,但是reduce任务的输出
	// 必须是JSON格式,熟悉JSON数据格式会对你有所帮助。你可以使用下面的代码将数据结构以JSON字符串的形式输出。对应的解码函数在common_reduce.go
	// 可以找到。
	//
	//   enc := json.NewEncoder(file)
	//   for _, kv := ... {
	//     err := enc.Encode(&kv)
	//
	//   记得关闭文件当你写完全部的数据之后。


	// 注：Map的大致流程如下(官方教材建议不上传代码，所以去除)
	// 　S1: 　打开输入文件，并且读取全部数据
	//  S2： 调用用户自定义的mapF函数,分检数据,在word count的案例中分割成单词
	//  S3： 将mapF返回的数据根据key分类,跟文件名对应(reduceName获取文件名)
	// 　S4: 　将分类好的数据分别写入不同文件

}

func ihash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
