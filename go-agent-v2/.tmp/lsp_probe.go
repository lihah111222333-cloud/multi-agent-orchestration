package lsp_probe

func Add(a, b int) int {
	return a + b
}

func Use() int {
	return Add(1, 2)
}
