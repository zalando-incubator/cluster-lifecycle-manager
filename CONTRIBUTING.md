# Contributing

**Thank you for your interest in making the project better and more useful.
Your contributions are highly welcome.**

There are multiple ways of getting involved:

- [Report a bug](#report-a-bug)
- [Suggest a feature](#suggest-a-feature)
- [Contribute code](#contribute-code)

Below are a few guidelines we would like you to follow.
If you need help, please reach out to us: team-teapot@zalando.de

## Report a bug

Reporting bugs is one of the best ways to contribute. Before creating a bug
report, please check that an [issue](../../issues) reporting the same problem does
not already exist. If there is an such an issue, you may add your information
as a comment.

To report a new bug you should open an issue that summarizes the bug and set
the label to "bug".

If you want to provide a fix along with your bug report: That is great! In this
case please send us a pull request as described in section [Contribute
Code](#contribute-code).

## Suggest a feature

To request a new feature you should first check if the feature is already
requested in one of the open [issues](../../issues). If not, then please open
an [issue](../../issues/new) and summarize the desired functionality and its
use case. Set the issue label to "feature".

## Contribute code

This is a rough outline of what the workflow for code contributions looks like:
- Check the list of open [issues](../../issues) Either assign an existing issue
  to yourself, or create a new one that you would like work on and discuss your
  ideas and use cases.
- Fork the repository on GitHub
- Create a topic branch from where you want to base your work. This is usually
  master.
- Make commits of logical units.
- Write good commit messages (see below).
- Sign/sign off your commits: `git commit -s ..`.
- Push your changes to a topic branch in your fork of the repository.
- Submit a pull request to the repository.
- Your pull request must receive a :thumbsup: from two [Maintainers](/MAINTAINERS).

Thanks for your contributions!

### Code style

Code is formatted with [gofmt](https://golang.org/cmd/gofmt/). Please run it
on your code before making a pull request. The coding style suggested by the
Golang community is the preferred one for the cases that are not covered by
gofmt, see the [style
doc](https://github.com/golang/go/wiki/CodeReviewComments) for details.

### Commit messages

Your commit messages ideally can answer two questions: what changed and why.
The subject line should feature the “what” and the body of the commit should
describe the “why”.

When creating a pull request, its description should reference the corresponding
issue id.

**Have fun and enjoy hacking!**
