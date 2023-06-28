master:
	cd ns-master && go build && cd ..

check:
	cd ns-check && go build && cd ..

all: master check
clean:
	rm -f ns-master/ns-master ns-check/ns-check
