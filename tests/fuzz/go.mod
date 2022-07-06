// Used to exclude it from Go mod in repository root.
// Replaced by oss_fuzz_build.sh.
module github.com/fluxcd/image-automation-controller/tests/fuzz

go 1.18

replace (
	github.com/fluxcd/image-automation-controller/api => ../../api
	github.com/fluxcd/image-automation-controller => ../../
)
