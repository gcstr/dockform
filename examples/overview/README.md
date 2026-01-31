# Manifest File Overview

```yaml
daemons:
  default:
    context: default
    identifier: overview

sops:
  age:
    key_file: ${AGE_KEY_FILE}
    recipients:
      - age1vmn3nv333mprv02cn8qyafxaz94zg368lnk5gsclmme9ryludysswn5rgr

stacks:
  default/website:
    root: website
    files:
      - docker-compose.yaml
    project:
      name: website
    environment:
      files:
        - vars.env
      inline:
        - FOO=bar
        - BAZ=qux
    secrets:
      sops:
        - secrets.env
```

Filesets and secrets can also be auto-discovered using convention-over-configuration.
Place compose files, secrets, and volume directories under `<daemon>/<stack>/` and they
will be picked up automatically without explicit manifest entries.
