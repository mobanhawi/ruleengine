# Rule Engine (Systema)

This repository contains a configurable rule engine using [CEL (Common Expression Language)](https://opensource.google/projects/cel) for flexible business logic and validation.

## Features

- **YAML-based configuration** for rules, rulesets, execution policies, and environment overrides
- **CEL expressions** for powerful, type-safe rule logic
- **Customizable error handling and logging** 
- **Environment-specific overrides** for development and production

## Example: Defining Rules

Rules are defined in `rules.yml` under the `rules:` section.  
Example:

```yaml
rules:
  age_validation:
    name: "Age Validation"
    description: "Validates user age requirements"
    expression: "user.age >= globals.min_age"

  email_format:
    name: "Email Format Check"
    description: "Validates email format using regex"
    expression: |
      user.email.matches("^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}$")
```

## Example: Combining Rules

Rulesets allow you to combine multiple rules using logical operators:

```yaml
rulesets:
  user_registration:
    name: "User Registration Validation"
    description: "All rules must pass for successful registration"
    combination_type: "AND" # OR is allowed, default is AND
    rules:
      - age_validation
      - email_format
      - user_status
```

## Execution Policies

Control how rules are executed:

```yaml
execution_policies:
  fail_fast:
    name: "Fail Fast Execution"
    stop_on_failure: true

  collect_all:
    name: "Collect All Results"
    stop_on_failure: false
    collect_errors: true
```

## Error Handling

Customize error handling and logging:

```yaml
error_handling:
  execution_policy: "collect_all"
  custom_error_messages:
    age_validation: "User must be at least 18 years old"
    email_format: "Please provide a valid email address"
    domain_whitelist: "Email domain is not allowed"
    business_hours: "Service only available during business hours (9 AM - 5 PM)"
```

## Environment Overrides

Override globals and policies per environment:

```yaml
environments:
  development:
    globals:
      min_age: 13 # Lower age requirement for testing

  production:
    globals:
      min_age: 18
    error_handling:
      default_policy: "fail_fast"
```

## Usage

To use the rule engine, load the configuration from `rules.yml`, set up the environment, and evaluate rules against input data `context`.

For more details, see the comments in `rules.yml` or consult the CEL documentation. 

## TODO
- Execution policy implementation
- Unit tests coverage
- Regex examples