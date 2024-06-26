# FRITZ!Box Cable Exporter

This is a Prometheus exporter for the FRITZ!Box DOCSIS 3.1 cable modem.
It gathers metrics by scraping the modem's web interface.

This was developed using a FRITZ!Box 6591 (Vodafone) in version 07.50, untested on other releases.

Please use previous tag / version as there were breaking changes with 07.50 release.

A scrape is pretty fast - configuration can be as following:

```yaml
  - job_name: 'fritzCable'
    scrape_interval: '1m'
    scrape_timeout: '55s'
    static_configs:
      - targets:
        - 'localhost:9623'
```

Known issues:

* Network metrics itself is not yet implemented
* Authentication works only with username + password!
* PBKDF2 is not yet supported (my test devices do not support the new challenge mode)

## Docker
Image is available at Github Packages via `ghcr.io/monofox/fritzcable_exporter:main`.

## Contribution
Your contributions are more than welcome. Just clone, change and make a pull-request.

## Thanks
Base was the tc4400_exporter by @markuslindenberg - thank you!
