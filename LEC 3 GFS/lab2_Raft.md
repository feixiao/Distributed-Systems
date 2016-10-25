## 6.824 Lab 2: Raft

[原文地址](https://pdos.csail.mit.edu/6.824/labs/lab-raft.html)

### 简介
+ 下面的一系列实验，你们会构建一个具有容错功能的kv存储系统，这是一系列实验的第一个。你们将会从实现Raft算法开始，a replicated state machine protocol（一个复制状态机协议？）. 在下一个实验中，你们将会在Raft算法之后构建KV服务。然后你们会分散你们的服务以换取更高的性能，最后实现分布式事务操作。
+ 一个复杂服务使用Raft协议有利于管理众多备份服务器。正是基于有备份服务器这一点，服务器在副本出错的情况（崩溃、a broken、糟糕的网络环境）也能继续操作。挑战也在这里，就是因为这种错误情况的存在，副本们不是总是保持数据一致性；Raft帮助服务挑选出哪些数据是正确的。
+ Raft基本的方法是实现了一个复杂的状态机。Raft将客户端请求组织成一个序列,称为日志，然后保证全部的副本同意日志的内容。每个副本按照日志中的请求顺序的执行，将这些日志里面的情况应用到本机服务。因为全部活着的副本看到一样的日志内容，它们都是顺序的执行一样的请求，因此它们有相同的服务状态。如果一个服务器失败了，之后又恢复了，Raft会小心翼翼地把它的日志更新到最新。只要多数的服务器可以工作，同时它们直接可以相互通信，Raft就可以工作。如果存活的服务不多了，那么Raft毫无进展，但是会等待多数服务存活的情况下继续工作。
+ 在这个实验中你们将会带有方法的Go对象实现Raft，这意味着将会作为一个更大服务的一个模块使用。一系列Raft实例通过RPC相互通信维护日志副本。你们的Raft接口将支持无顺序编号的命令，同时这些命令被称为日志节点（ log entries）。节点被使用数字索引。带有索引的日志节点最终将会被提交。在那个阶段，也就是你们的Raft应该发生日志节点到更大的服务去执行。

		注意： 不同Raft实例直接的交互我们只使用RPC。举例，不同的Raft实例直接不允许通过共享Go变量的方式交互。当然你们的实现也不能使用文件。

+ 在这个实验中你们的实现[《extended Raft paper》](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)中描述的大多数设计，包括持久化状态，然后当服务器失败重启的时候读取持久化数据。你们不会实现集群关系更改或者日志压缩/快照。
+ 你们应该总结[《extended Raft paper》](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)和Raft课程讲稿。你们也许会发现[《illustrated Raft guide 》](http://thesecretlivesofdata.com/raft/)有利于高层次的理解Raft的工作。为了更广阔的视角，应该去了解 Paxos, Chubby, Paxos Made Live, Spanner, Zookeeper, Harp, Viewstamped Replication,和[Bolosky et al](http://static.usenix.org/event/nsdi11/tech/full_papers/Bolosky.pdf).

	+  提示： 尽早开始。虽然实现部分代码不是很多，但是让它正常的工作将会是一个挑战。算法和代码都非常狡猾，同时还有很多偏僻的个案需要你们考虑。当一个测试失败的时候，也许比较费解到底是哪个场景让你们的解决方案不正确，怎么去修改你们的解决方案。
	+  提示： 在你开始之前阅读理解[《extended Raft paper》](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)和Raft课堂讲稿。你们的实现应该贴近论文的描述，因为那也是测试因为的。Figure 2部分的伪代码应该会有所帮助。

### 合作政策
+ 你们必须编写课程6.824出来我们提供的的全部代码，不能查看其他人的解决方案，也不能查看上一届的代码实现，也不允许查看其他Raft的实现。你们也许会跟其他学习讨论，反射不能查看或者直接复制他们的代码。请不要公开你的代码而被这门课程的学生所使用。比如，不要将你的代码上传到Github。[我这样不传代码应该没事吧]()

### 开始
+ 使用git pull命令获取最新的实验代码。我们在src/raft目录下面为你们提供框架代码和测试，在src/labrpc目录下面提供了一个简单的类rpc系统。
+ 获取代码，然后运行，执行下面的命令。
		$ setup ggo_v1.5
		$ cd ~/6.824
		$ git pull
		...
		$ cd src/raft
		$ GOPATH=~/6.824  // 根据实际情况填写
		$ export GOPATH
		$ go test
		Test: initial election ...
		--- FAIL: TestInitialElection (5.03s)
		config.go:270: expected one leader, got 0
		Test: election after network failure ...
		--- FAIL: TestReElection (5.03s)
		config.go:270: expected one leader, got 0
		...
		$
+ 当你们全部完成的时候，你们的实现应该全部src/raft目录下面的测试：
		$ go test
		Test: initial election ...
  		... Passed
		Test: election after network failure ...
		  ... Passed
		...
		PASS
		ok  	raft	162.413s


### 你们的工作
+ 你们通过在raft/raft.go文件里面添加代码实现Raft。在那个文件里面，你们会发现一些框架代码，添加发生和接收RPC请求的例子，添加保存恢复状态的例子代码。
+ 你们的实现必须支持下面的接口，这些接口会在测试例子和你们最终的key/value服务器中使用。你们可以在raft.go里面获取更多的细节。
	
    	// create a new Raft server instance:
		rf := Make(peers, me, persister, applyCh)

		// start agreement on a new log entry:
		rf.Start(command interface{}) (index, term, isleader)

		// ask a Raft for its current term, and whether it thinks it is leader
		rf.GetState() (term, isLeader)

		// each time a new entry is committed to the log, each Raft peer
		// should send an ApplyMsg to the service (or tester).
		type ApplyMsg

+ 一个服务通过调用Make(peers,me,…)创建一个Raft端点。peers参数是通往其他Raft端点处于连接状态下的RPC连接。me参数是自己在端点数组中的索引。Start(command)要求Raft开始将command命令追加到日志备份中。Start()函数马上返回，不等待处理完成。服务期待你们的实现发生一个ApplyMsg结构给每个完全提交的日志，通过applyCh通道。
+ 你们的Raft端点应该使用我们提供的librpc包来交换RPC调用。它是仿照Go的rpc库完成的，但是内部使用Go channles而不是socket。raft.go里面包含了一些发生RPC(sendRequestVote())和处理RPC请求（RequestVote()）的例子代码。

		任务：实现领导选举和心跳(empty AppendEntries calls). 这应该是足够一个领导人当选,并在出错的情况下保持领导者。一旦你们让下面的正常工作，你们就可以通过第一二个测试。



















+  
+ 

