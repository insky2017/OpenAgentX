独立审查原文与 overlay 保留原始证据目录及文件名。入库时仅将 `independent_integration_test.go` 改名为 `.go.txt`，字节及 SHA 不变，防止 Go 将只用于 overlay 的证据当作独立测试包发现。原始 manifest 的对应键随公开文件名调整，所有公开文件另受上级 SHA256SUMS 校验。
