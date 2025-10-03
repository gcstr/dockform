# Manifest File Overview

```yaml
docker:
  context: default
  identifier: overview

sops:
  age:
    key_file: ${AGE_KEY_FILE}
    recipients: 
      - age1vmn3nv333mprv02cn8qyafxaz94zg368lnk5gsclmme9ryludysswn5rgr

secrets:
  sops:
    - secrets.env

environment:
  files:
    - global.env
  inline:
    - GFOO=bar
    - GBAZ=qux

applications:
  website:
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

filesets:
  files:
    source: website/assets/
    target_volume: demo-volume-1
    target_path: /assets
    restart_services:
      - nginx
    exclude:
      - "**/.DS_Store"
      - "*.bak"
      - "tmp/**"
      - "secrets/"
      # - "**/*.svg"

volumes:
  my-volume:

networks:
  demo-network:
```
