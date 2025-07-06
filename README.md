<h1 align="center">PromSketch: Approximation-First Timeseries Qery At Scale</h1>

## About

<p align="center"> <img src="./doc/images/prometheus_diagram.png" alt="" width="600"> </p>

This repository provides PromSketch package for Prometheus and VictoriaMetrics integrations.


## Quick Start
### Install Dependencies
```
# installs Golang
wget https://go.dev/dl/go1.22.4.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.22.4.linux-amd64.tar.gz
export PATH=$PATH:/usr/local/go/bin
```

```
# installs nvm (Node Version Manager)
curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.0/install.sh | bash
# download and install Node.js (you may need to restart the terminal)
nvm install 20
```

### Datasets
* Goolge Cluster Data v1: https://github.com/google/cluster-data/blob/master/TraceVersion1.md
* Power dataset: https://www.kaggle.com/datasets/uciml/electric-power-consumption-data-set?resource=download
* CAIDA traces: https://www.caida.org/catalog/datasets/passive_dataset_download/

### Run EHUniv test
```
cd promsketch
go test -v -timeout 0 -run ^TestExpoHistogramUnivMonOptimizedCAIDA$ github.com/froot/promsketch
```

### Run EHKLL test
```
cd promsketch
go test -v -timeout 0 -run ^TestCostAnalysisQuantile$ github.com/froot/promsketch
```

### Integration with Prometheus

```
git clone git@github.com:zzylol/prometheus-sketches.git
```
Compile:
```
cd prometheus-sketches
make build
```

### Integration with VictoriaMetrics single-node version

```
git clone git@github.com:zzylol/VictoriaMetrics.git
```
Compile:
```
cd VictoriaMetrics
make victoria-metrics
make vmalert
```

### Integration with VictoriaMetrics Cluster version
https://github.com/zzylol/VictoriaMetrics-cluster

## Citation
Please consider citing this work if you find the repository helpful.
```
@article{zhu2025approximation,
  title={Approximation-First Timeseries Monitoring Query At Scale},
  author={Zhu, Zeying and Chamberlain, Jonathan and Wu, Kenny and Starobinski, David and Liu, Zaoxing},
  journal={arXiv preprint arXiv:2505.10560},
  year={2025}
}
```
