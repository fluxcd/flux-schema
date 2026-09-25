---
weight: 55
---

# Flux Schema JUnit XML output

The `flux schema validate` command can emit validation results as
[JUnit XML](https://github.com/testmoapp/junitxml) using `-o junit`.
This format is particularly useful in CI/CD pipelines because many CI systems
can consume JUnit reports and present test results directly in their user
interface.

Instead of parsing the command's console output, a CI pipeline can publish the
JUnit report as a test report.
Validation errors then appear alongside other test failures, typically with the
failed resource and validation message available for inspection.

For resources rendered by a kustomize build, the testcase `file` is the
resource's `origin` when it has one, otherwise the kustomization file.

**Warning:** The JUnit XML format is not standardized. There is no single
formal standard for the JUnit XML format, and CI systems
may interpret the format differently. The `flux schema validate` JUnit output
is under development, and we will aim to make it as broadly compatible as
possible, with particular focus on GitLab and publicly available GitHub
Actions that support JUnit reports. The exact structure of the generated XML
may change as compatibility and interoperability are improved.

## Generating a JUnit report

Use `-o junit` to write the validation results as JUnit XML:

```shell
flux schema validate ./manifests -o junit > report.xml
```

For example, GitLab CI can collect the report with:

```yaml
flux-validate:
  script:
    - flux schema validate ./manifests -o junit > report.xml
  artifacts:
    reports:
      junit: report.xml
```

This allows validation failures to be displayed as test failures in the GitLab
pipeline or merge request UI rather than requiring users to inspect the job log.

## Writing reports to a directory

JUnit reports can also be written to a directory using `--output-dir` (`-d`):

```shell
flux schema validate ./manifests -o junit -d /reports
```

This is useful when a CI job produces multiple reports or when the report
directory is already used as an artifact location.

## When to use JUnit

Use JUnit output when validation results need to be consumed by CI/CD tooling.
It is especially useful when:

- validation results should appear as test results in the CI interface;
- failed validations should be easy to identify and inspect;
- validation is one of several test or quality checks in a pipeline;
- the CI system already supports publishing JUnit reports.

For integrations that need to process the complete structured validation report
programmatically, `json` or `yaml` may be more appropriate.
The JSON and YAML formats use the versioned report structure documented by
[`report-v1beta1.json`](report-v1beta1.json), whereas JUnit XML is intended
primarily for interoperability with CI/CD test-report tooling.
