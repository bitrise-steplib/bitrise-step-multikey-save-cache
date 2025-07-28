### Examples

Check out [Workflow Recipes](https://github.com/bitrise-io/workflow-recipes?tab=readme-ov-file#-caching) for platform-specific examples!

#### Skip saving the cache in PR builds (only restore)

```yaml
steps:
- multikey-restore-cache@1:
    inputs:
    - keys: |-
        node-modules-{{ checksum "package-lock.json" }}

# Build steps

- multikey-save-cache@1:
    run_if: ".IsCI | and (not .IsPR)" # Condition that is false in PR builds
    inputs:
    - key_path_pairs: |-
          node-modules-{{ checksum "package-lock.json" }} = node_modules
```

#### Separate caches for each OS and architecture

Cache is not guaranteed to work across different Bitrise Stacks (different OS or same OS but different CPU architecture). If a Workflow runs on different stacks, it's a good idea to include the OS and architecture in the **Cache key** input:

```yaml
steps:
- multikey-save-cache@1:
    inputs:
    - key_path_pairs: |-
        {{ .OS }}-{{ .Arch }}-npm-cache-{{ checksum "package-lock.json" }} = node_modules
```

#### Multiple independent caches

You can add multiple instances of this Step to a Workflow:

```yaml
steps:
- multikey-save-cache@1:
    title: Save cache
    inputs:
    - key_path_pairs: |-
        node-modules-{{ checksum "package-lock.json" }} = node_modules
        pip-packages-{{ checksum "requirements.txt" }} = venv/
```
