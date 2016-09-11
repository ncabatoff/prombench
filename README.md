# prombench
Benchmark and integration testing tool for Prometheus

For now let's just worry about benchmarking ingestion and verifying that it worked.

Generate load in the form of synthetic exporters that produce a configurable
size metric and target set.

When the test run completes, query Prometheus for the metrics exported to make
sure that they were correctly ingested.

The eventual goal is to have Prombench monitor the ingestion queue of the
Prometheus instance being tested and increases the load by adding new exporters
until Prometheus can't keep up, then throttle back until it's just below the
tipping point.  For now we're just generating a static load.

Some Prometheus metrics we could use to achieve this goal:

prometheus\_local\_storage\_indexing\_queue\_length The number of metrics waiting to be indexed.

prometheus\_local\_storage\_chunks\_to\_persist The current number of chunks waiting for persistence.

