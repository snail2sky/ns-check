# ns-check
Regularly check the health status of the nameserver in /etc/resolv.conf

## introduction
- The ns-check program will be check `-resolv-conf` specified file and extract the nameserver, get more nameserver from `-endpoint-url` and `-default-nameserver`. These nameservers will be detected concurrently. The timeout period is defined by the `-ns-check-timeout`. After the detection is completed, they will be sorted according to the delay, Finally retain the nameserver specified by the `-max-nameservers`, All the above operations will be repeated according to the specified `-interval`, The `-options` and `-search` will be write to `-resolv-conf`. `-fetch-timeout` is request `-endpoint-url` timeout.

- The ns-master is a sample program to provide more nameservers to the ns-check program. 
    - The returned interface data is as follows
    - ```json
      {
        "nameservers": ["1.1.1.1", "2.2.2.2"],
        "endpointURL": "http://127.0.0.1:5353/nameservers"
      }
      ```

## build
```bash
# ns-check program
cd ns-check
go build
./ns-check -h

# ns-master program
cd ns-master
go build
./ns-master -h
```

## command line parameter

### ns-check
```bash
./ns-check -h
Usage of ./ns-check:
  -default-nameserver string
        Default nameserver fallback (default "8.8.8.8,8.8.4.4,1.1.1.1")
  -endpoint-url string
        URL for fetching nameservers if resolv.conf is unavailable (default "http://127.0.0.1:5353/nameservers")
  -fetch-timeout duration
        Timeout for fetch data from endpoint url (default 2s)
  -interval duration
        Interval between each round of detection (default 30s)
  -max-nameservers int
        Maximum number of nameservers to write back to resolv.conf (default 3)
  -ns-check-timeout duration
        Timeout for nameserver connectivity check (default 2s)
  -options string
        Options field in resolv.conf (default "timeout:1 attempts:1")
  -resolv-conf string
        Path to resolv.conf file (default "/etc/resolv.conf")
  -search string
        Search field in resolv.conf (default "localhost")
```


### ns-master
```bash
./ns-master -h
Usage of ./ns-master:
  -endpoint string
        Endpoint URL for fetching nameservers (default "/nameservers")
  -endpoint-url string
        Endpoint url will used by client (default "http://127.0.0.1:5353/nameservers")
  -nameservers string
        Comma-separated list of nameservers (default "8.8.8.8,8.8.4.4,1.1.1.1")
  -port int
        Port number for the server (default 5353
```