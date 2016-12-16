## 6.824 Lab 1: MapReduce

[原文地址](https://pdos.csail.mit.edu/6.824/labs/lab-1.html)

### 简介
在这实验中你们将会通过建立MapReduce的库方法去学习Golang编程语言，同时学习分布式系统怎么进行容错。在第一部分你将会编写简单的MapReduce程序。在第二部分你将会编写master，master往workers分配任务，处理workers的错误。库的接口和容错的方式跟之前MapReduce论文中描述的很类似。

### 合作政策
你们必须亲手编写课程6.824的全部代码，除非我们只是给你们分配了一部分。不能抄袭其他人的解决方案，同时也不能看上一年的课程代码。你们也许会和其他学生讨论作业，但是不能参考或者抄袭对方的代码。请不要公布你们的代码或者不要向以后的学生透露，比如请不要讲你们的代码以可见的方式放置在github上面。

### 软件
你们将会使用Golang语言实现这个以及其他的全部实验。Golang的官方网站包含了很多教程资料，也许你们会想去查看资料。

我们将会向你们提供MapReduce的部分实现，包括分布式和非分布式的操作。你将通过git(一个版本管理系统)获取到初始的实验代码。可与参考它的帮助手册学习如何使用git，或者如果你已经熟悉其他版本控制软件，你也许会发现this CS-oriented overview of git useful.

课程的git仓库地址在it://g.csail.mit.edu/6.824-golabs-2016。使用你们的Athena账号(MIT Kerberos account的别称)安装这些文件。通过下面的命令克隆课程的代码，你们必须使用x86或者x86_64的机器;那是，使用uname -a会出现i386 GNU/Linux 或者i686 GNU/Linux或者x86_64 GNU/Linux说明的机器。你可以通过athena.dialup.mit.edu域名登入一台i686的机器。

	$ add git # only needed on Athena machines
	$ git clone git://g.csail.mit.edu/6.824-golabs-2016 6.824
	$ cd 6.824
	$ ls
	Makefile src

Git会帮助你追踪你对代码的修改情况。例如,如果你们向检查你的进度，你们可与提交你们的变更通过执行下面的命令：

	$ git commit -am 'partial solution to lab 1'

我们给你们的Map/Reduce包括两中模式的操作，顺序的和分布式的。在第一种情况下，Map和Reduce任务都是窜行执行的。首先，第一个map任务执行完成在执行第二个Map，然后再是第三个Map等等，等到全部的Map任务完成，第一个Reduce任务开始执行，然后第二个第三个。在这种模型下，虽然执行不是很快，但是很方便调试程序，因为它去除了并行运行中很多麻烦的地方。分布式模式下，运行很多worker线程首先并行的执行Map任务，然后再执行Reudce任务。这种模型执行会快很多，但是很难实现，同时很难调试。

### 序言： 熟悉源代码
mapreduce包一顺序的方式实现了简单的Map/Reduce库。应用程序应该调用Distributed函数(位于master.go)启动一个工作，但是也许会调用Sequential方法替换(同样在master.go)
Preamble: Getting familiar with the source

mapreduce的实现如下所述：
+ 1：应用程序提供很多输入文件，map函数，reduce函数和很多Reduce任务(nReduce)
+ 2：master基于这些知识而建立。master是一个RPC的服务器(查看master_rpc.go文件)，等待workers来向它注册(使用在master.go中定义的RPC方法)。当任务变得可用(在第4步和第5步的时候)，schedule方法(定义在schedule.go中)决定如果将这些任务分配给workers，还有就是怎么处理workers的错误。
+ 3：master将每一个输入文件作为一个map任务，然后每个任务至少调用一次doMap方法(定义在common_map.go)。任务直接运行(当调用Sequential方法)或者通过触发worker上的DoTask RPC调用(worker.go)。每一个doMap会调用合适的文件，在文件内容上调用map函数，然后为每个map文件产生nReduce文件。因此，在全部的map任务执行完成之后这里将会有#files x nReduce文件。
		f0-0, ..., f0-0, f0-, ...,
    	f<#files-1>-0, ... f<#files-1>-.
+ 4：master的下一步工作就是为reduce任务至少调用一次doReduce函数(common_reduce.go)，和doMap函数一样，它可以直接运行或者通过worker运行。doReduce方法收集每一个map产生的reduce文件，然后在这些文件之上调用reduce函数。这些产生最后的nReduce个结果文件。
+ 5：master调用mr.merge方法(master_splitmerge.go),它将会合并上面产生的全部结果文件到一个输出。
+ 6: master发生shutdown的RPC调用给workers，然后关闭自己的RPC服务器。

	  通过课程的下面练习，你将会自己编写或者修改doMap,doReduce和schedule。这些分别在common_map.go，common_reduce.go和schedule.go文件中。你也必须编写../main/wc.go中的map函数和reduce函数。

你不需要更改其他文件，但是阅读它们可与帮你理解其他的方法适应系统的总体架构。


### 第一部分: Map/Reduce 输入和输出
你们获取的Map/Reduce实现缺少部分代码。在你能编写自己第一个map/reduce函数对之前，你们需要完善顺序的实现。特别的，我们提供的代码缺少两个重要部分：将输出分割成map任务的函数和为reduce任务收集全部输入的函数。这些任务将分别会被common_map.go里面的doMap函数和common_reduce.go里面的doReduce函数执行。这些文件中的注解应该会帮你正确理解代码。

为了帮助你们判断你们是否正确的实现doMap函数和doReduce函数，我们向你提供了go测试包，它会帮你们检查实现的正确性。这些测试实现全部在文件test_test.go里面。为了运行你已经完善的顺序实现的测试代码，需要执行下面的命令：

	$ cd 6.824
	$ export "GOPATH=$PWD"  # go needs $GOPATH to be set to the project's working directory
	$ cd "$GOPATH/src/mapreduce"
	$ setup ggo_v1.5
	$ go test -run Sequential mapreduce/... 
		ok  	mapreduce	2.694s
当我们在我们的机器上面运行你们的程序，如果你们的软件通过了顺序实现的测试(就是运行上面的命令)，你们将获取这个部分的全部学分。

如果测试的输出不是ok，那么你们的实现还存在bug。为了获取更多的详细信息，在common.go文件中的degubEnabled设置为true，同时在上面的test命令里面加-v。这样你们将获取更多的输出。

	$ env "GOPATH=$PWD/../../" go test -v -run Sequential mapreduce/... 
	=== RUN   TestSequentialSingle
	master: Starting Map/Reduce task test
	Merge: read mrtmp.test-res-0
	master: Map/Reduce task completed
	--- PASS: TestSequentialSingle (1.34s)
	=== RUN   TestSequentialMany
	master: Starting Map/Reduce task test
	Merge: read mrtmp.test-res-0
	Merge: read mrtmp.test-res-1
	Merge: read mrtmp.test-res-2
	master: Map/Reduce task completed
	--- PASS: TestSequentialMany (1.33s)
	PASS
	ok  	mapreduce	2.672s

#### 第二部分：Single-worker word count
既然map和reduce任务已经被连接，那么我们开始实现一些Map/Reduce操作。在这个实验中我们会实现单词计数器——一个简单经典的Map/Reduce例子。特别地,你们的任务是改写mapF和reduceF函数，这样wc.go就可以报告每个单词的数量了。一个单词是任何相邻的字母序列，正如unicode.IsLetter定义的那样。

这里有一些以pg-开头的输入文件位于~/6.824/src/main路径下面。尝试编译我们通过给你的软件，然后就使用我们提供的输入文件运行看看：
		
    $ cd 6.824
    $ export "GOPATH=$PWD"
	$ cd "$GOPATH/src/main"
	$ go run wc.go master sequential pg-*.txt
	# command-line-arguments
	./wc.go:14: missing return at end of function
	./wc.go:21: missing return at end of function
编译失败的原因是我们还没有编写完整的map函数(mapF)和reduce函数(reduceF)。在你开始阅读MapReduce论文的第二部分时，这里需要说明你的mapF函数和reduceF函数跟论文中有些区别。你的mapF函数会传递文件名和文件的内容；它将会分隔成单词，然后返回含有键值对的切片类型([]KeyValue),你的reduceF函数对于每个键都会调用一次，参数是mapF生成的切片数据，它只有一个返回值。

你可以通过下面的命令运行你的解决方案：

	$ cd "$GOPATH/src/main"
	$ go run wc.go master sequential pg-*.txt
	master: Starting Map/Reduce task wcseq
	Merge: read mrtmp.wcseq-res-0
	Merge: read mrtmp.wcseq-res-1
	Merge: read mrtmp.wcseq-res-2
	master: Map/Reduce task completed
	14.59user 3.78system 0:14.81elapsed

生产的结果会在文件mrtmp.wcseq中，通过下面的命令删除全部的中间数据：

	$ rm mrtmp.*

如果执行下面的命令出现如下内容，那么你的实现是正确的：

	$ sort -n -k2 mrtmp.wcseq | tail -10
	he: 34077
	was: 37044
	that: 37495
	I: 44502
	in: 46092
	a: 60558
	to: 74357
	of: 79727
	and: 93990
	the: 154024

为了让测试更加简单，你可以运行下面的脚本：

	$ sh ./test-wc.sh

脚本会告知你的解决方案是否正确。

如果你的Map/Reduce单词计数器输出的结果和上面的一致的话，你可以得到全部的学分。如果想知道Go语言中的string是什么的话，可以阅读Go博客。你可以使用strings.FieldsFunc分离字符串。strconv包可以提供字符串到整xing型的转换等功能。

### 第三部分： 分布式MapReduce任务
Map/Reduce一个大的卖点是开发者不需要在多台机器上面并行运行它们的代码。在理论上，我们可以运在本地运行word count,然后可以自动并行化。

我们现在的实现是在同一个master上面一个接着一个的运行全部的map和reduce任务。虽然在概念上这种方法十分简单，但是性能不是很好。在这部分实验中，你将会完成另外一个版本的MapReduce,将工作分散到一系列的工作线程，为了充分利用多核的作用。虽然工作不是分布在真正的多机上面，不过你的实现可以使用RPC和Channel来模拟一个真正的分布式实现。

为了协调任务的并行执行，我们将会使用一个特殊的线程，它将会给workers分配工作，然后等待它们完成。为了让实验更加真实，master只能通过ＲＰＣ和workers交互。我们在mapreduce/worker.go文件中提供了worker的代码，代码会启动workers,然后处理ＲＰＣ消息(mapreduce/common_rpc.go)。

你的工作是完成mapreduce包中的schedule.go。尤其，你需要修改schedule.go文件中的schedule()函数用于分配map和reduce任务给workers，直到全部任务完成才返回。

查看master.go文件里面的ｒｕｎ()方法。它调用你的schedule()方法去运行map和reduce任务，然后调用merge()方法去收集之前reduce任务的输出到单个输出文件的输出结果。schedule只需要告诉workers原始输入文件的名字（mr.files[task]）和任务；每个worker都知道需要从哪个文件读取数据和输出结果到哪个文件。master通过RPC调用Worker.DoTask带有DoTaskArgs参数，给worker分配新任务。

当一个worker启动时,它会给master发送Register　RPC。mapreduce.go文件里面已经实现了Master.Register RPC的处理函数，你会将worker的信息发送给mr.registerChannel。你的schedule需要从读取并处理新worker的注册信息。


当前运行工作的信息会保存在Master结构体，定义在master.go。注意，master不需要知道哪个Map或者Reduce函数被这个工作使用;workers将会执行Map和Reduce的代码。mapreduce实现过程如下：

为了测试你的解决方案，你应该使用跟第一部分一样的Go测试工具，只是使用-run TestBasic替换 -run Sequential。它将会执行分布式的测试案例，但是不带有worker的错误处理。

	$ go test -run TestBasic mapreduce/...

你将会获取到这个部分的全部积分，如果你的MapReduce实现如下：通过test_test.go中的TestBasic测试。

	　　master应该并行地发送RPCs调用给ｗorker这样worker就可以并行的执行任务。你会发现ｇｏ的声明和Go RPC文档会非常有帮助。
    　　master在处理其他任务前必须等待work完成。你也许会发现channels对于同步线程十分有用。Channels在Gｏ文档的Concurrency部分说明。
	　　最简单的追查bug的方法是插入debug() 状态，收集输出到一个文件使用命令go test -run TestBasic mapreduce/... > out，然后思考输出结果是否跟你预期的代码行为相匹配。最后一步(思考)是最重要的。

我们提供代码你可以以单个UNIX进程的方式运行workers,充分发挥单机上面多核的作用。为了让workers可以通过网络交互并运行在多台机器上面，有一些修改是必须的。RPCs应该使用TCP而不是UNIX-domain sockets;应该存在方法能够让worker在全部的机器上面运行;全部的机器应该通过某种网络文件系统共享存储。

#### 第四部分：处理worker的错误
在这个部分你将会使master处理workers的失败。因为workers没有可持久化状态，所以MapReduce处理workers的失败相对比较简单。如果一个worker失败了，任何master对它的调用都会失败。因此，如果master对于worker的RPC调用失败，那么master应该重写分配任务到其他worker。

一个RPC的失败并不一定就意味着worker失败，也许worker只是现在不可达，但是它依然还在执行计算。因此，可能发生两个worker接受到同样的任务然后进行计算。但是，因为任务是幂等的，所以同一个任务计算两遍也没有关系－－两次输出都是相同的结果。所以你不需要针对这个案例做任何特殊的操作。（我们的测试永远不会在任务的中间失败，所以你根本不需要担心一些workers输入到同一个文件。）

你不需要处理master的失败;我们假设它不会失败。让master进行容错处理将会非常难，因为它保存了持久化的状态，它们将会被用来恢复master的错误。后面的实验致力于完成这些调整。

你的实现必须通过test_test.go中的两个测试案例。第一个测试案例是一个worker的错误处理，第二个测试案例是多个worker的错误处理。测试案例会定期的启动新的worker，这样the master can use to make forward progress，but these workers fail after handling a few tasks. To run these tests:

	$ go test -run Failure mapreduce/...

当我们通过执行上面的命令运行你的程序，如果你的软件通过了这些测试，你将会获取到这个部分的全部积分。

#### 第五部分：反向索引Inverted index generation (optional)
Word count是一个典型的Map/Reduce应用，但是它不是一个大规模消费者使用Map/Reduce的例子。它非常简单，现实中很少需要你从一个大的数据集里面统计单词的数量。在这个具有挑战性的练习中，我们代替你已经建立用于生成反向索引的Map和Reduce函数。

反向索引在计算机科学领域被广泛使用，尤其在文档搜索方面。广义地说，一个反向索引就是map,存在底层数据的一些有趣的事实，说明数据的原始位置。例如，在文本搜索的时候，反向索引也许是这样的map,它包含了两个单词从关键字指向文档.??

我们将会建立第二个可执行程序在main/ii.go，跟你之前的建立的wc.go非常类似。你应该修改main/ii.go文件中的mapF和reduceF函数，那么它们就可与共同产生反向索引。运行ii.go将会输出一些元组，每一行都类似下面这种格式：

	$ go run ii.go master sequential pg-*.txt
	$ head -n5 mrtmp.iiseq
	A: 16 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	ABC: 2 pg-les_miserables.txt,pg-war_and_peace.txt
	ABOUT: 2 pg-moby_dick.txt,pg-tom_sawyer.txt
	ABRAHAM: 1 pg-dracula.txt
	ABSOLUTE: 1 pg-les_miserables.txt

如果上面的列表不是很清楚的话，格式如下:

    word: #documents documents,sorted,and,separated,by,commas

你必须通过test-ii.sh才能获取到这个挑战的全部积分，通过运行下面的命令：

	$ sort -k1,1 mrtmp.iiseq | sort -snk2,2 mrtmp.iiseq | grep -v '16' | tail -10
	women: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-	emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	won: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	wonderful: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	words: 15 pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	worked: 15 pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	worse: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	wounded: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	yes: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-metamorphosis.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	younger: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt
	yours: 15 pg-being_ernest.txt,pg-dorian_gray.txt,pg-dracula.txt,pg-emma.txt,pg-frankenstein.txt,pg-great_expectations.txt,pg-grimm.txt,pg-huckleberry_finn.txt,pg-les_miserables.txt,pg-moby_dick.txt,pg-sherlock_holmes.txt,pg-tale_of_two_cities.txt,pg-tom_sawyer.txt,pg-ulysses.txt,pg-war_and_peace.txt

####  运行全部的测试
你可与运行全部的测试通过运行脚本src/main/test-mr.sh。如果你的解决方案正确，你的输出会类似这样：

	$ sh ./test-mr.sh
	==> Part I
	ok  	mapreduce	3.053s

	==> Part II
	Passed test

	==> Part III
	ok  	mapreduce	1.851s

	==> Part IV
	ok  	mapreduce	10.650s

	==> Part V (challenge)
	Passed test

#### 提交部分就不翻译了
