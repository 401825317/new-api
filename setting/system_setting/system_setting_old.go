package system_setting

var ServerAddress = "http://localhost:3000"
var VideoProxyAddress = ""
var VideoProxySignSecret = ""
var VideoResultURLMode = "proxy"
var WorkerUrl = ""
var WorkerValidKey = ""
var WorkerAllowHttpImageRequestEnabled = false

func EnableWorker() bool {
	return WorkerUrl != ""
}
