# `groktest`

The `groktest` command provide a command line interface for quickly testing Elasticsearch ingest pipeline grok processors.

`groktest` requires that the `grok` tool be in your `$PATH`.

## Example

Grab a grok processor fragment from a pipeline.
```yaml
grok:
  patterns:
  - "%{IPV4:net.ip:ip}/%{MASK:net.cidr:int}"
  - "%{IPV6:net.ip:ip}/%{MASK:net.cidr:int}"
  pattern_definitions:
    MASK: "[0-9]+"
```
and a file to examine
```
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
       valid_lft forever preferred_lft forever
    inet6 ::1/128 scope host 
       valid_lft forever preferred_lft forever
```
Then either run as one shot which will get all the matches.
```
$ groktest -grok ip4.yaml -path addr.text
{ "@LINE": "    inet 127.0.0.1\/8 scope host lo", "@MATCH": "127.0.0.1\/8", "IPV4:net_ip_ip": "127.0.0.1", "MASK:net_cidr_int": "8" }
{ "@LINE": "    inet6 ::1\/128 scope host ", "@MATCH": "::1\/128", "IPV6:net_ip_ip": "::1", "MASK:net_cidr_int": "128" }
```
or run the program requiring that all lines match
```
$ groktest -grok ip4.yaml -path addr.text -all
no match: 1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN group default qlen 1000
no match:     link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
{ "@LINE": "    inet 127.0.0.1\/8 scope host lo", "@MATCH": "127.0.0.1\/8", "IPV4:net_ip_ip": "127.0.0.1", "MASK:net_cidr_int": "8" }
no match:        valid_lft forever preferred_lft forever
{ "@LINE": "    inet6 ::1\/128 scope host ", "@MATCH": "::1\/128", "IPV6:net_ip_ip": "::1", "MASK:net_cidr_int": "128" }
no match:        valid_lft forever preferred_lft forever
2022/04/22 18:14:03 grok failed: failed to match all inputs
exit status 1
```
which will give an non-zero exit status for any mismatch and will highlight lines that do not match.

To avoid having to cut text from a pipeline, it is possible to specify a line in a pipeline description like so, `groktest -grok file.yaml:<line>`. The specified line must be the first line of the grok processor.

## Note

Semantics are partially retained, but are mangled to prevent truncation by `grok`, for example above `IPV6:net.ip:ip` becomes `IPV6:net_ip_ip`. This may change in future. The output format is subject to change and should not be relied upon.