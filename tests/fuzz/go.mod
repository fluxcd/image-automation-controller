module github.com/fluxcd/image-automation-controller/tests/fuzz

go 1.17

replace (
	github.com/fluxcd/image-automation-controller/api => ../../api
	github.com/fluxcd/image-automation-controller => ../../
)
