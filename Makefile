all: 
	bash -c "export GOPATH=`pwd` && go install -v ./..."

get_human:
	bash -c "export GOPATH=`pwd` && go get github.com/dustin/go-humanize"
