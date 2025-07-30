import sys

from enum import Enum
from github import Github
from github import Auth
import re, sys
from github import UnknownObjectException

emojiFeature = str('✨')
emojiBugfix = str('🐛')
emojiDocs = str('📖')
emojiInfra = str('🌱')
emojiBreaking = str('⚠')


class PRType(Enum):
    UncategorizedPR = 1
    BreakingPR = 2
    FeaturePR = 3
    BugfixPR = 4
    DocsPR = 5
    InfraPR = 6


def pr_type_from_title(pr_title: str):
    t = pr_title.strip()
    if len(pr_title) == 0:
        return PRType.UncategorizedPR, t

    if pr_title.startswith(':sparkles:') or pr_title.startswith(emojiFeature):
        return PRType.FeaturePR, t.removeprefix(':sparkles:').removeprefix(emojiFeature).strip()
    elif pr_title.startswith(':bug:') or pr_title.startswith(emojiBugfix):
        return PRType.BugfixPR, t.removeprefix(':bug:').removeprefix(emojiBugfix).strip()
    elif pr_title.startswith(':book:') or pr_title.startswith(emojiDocs):
        return PRType.DocsPR, t.removeprefix(':book:').removeprefix(emojiDocs).strip()
    elif pr_title.startswith(':seedling:') or pr_title.startswith(emojiInfra):
        return PRType.InfraPR, t.removeprefix(':seedling:').removeprefix(emojiInfra).strip()
    elif pr_title.startswith(':warning:') or pr_title.startswith(emojiBreaking):
        return PRType.BreakingPR, t.removeprefix(':warning:').removeprefix(emojiBreaking).strip()

    return PRType.UncategorizedPR, t


def change_entry(pr_title, number, author):
    return "%s (#%d) @%s" % (pr_title, number, author)


def section_if_present(changes: [], pr_title):
    if len(changes) > 0:
        print("")
        print("## %s\n" % pr_title)
        print("")
        for change in changes:
            print("- %s\n" % change)


if __name__ == '__main__':
    args = sys.argv[1:]
    auth = Auth.Token(args[0])
    release_tag = args[1]
    g = Github(auth=auth)
    repo = g.get_repo("open-cluster-management-io/ocm")

    # Determine the branch based on release type
    # Validate semantic-version format (vX.Y.Z)
    if not re.match(r'^v\d+\.\d+\.\d+$', release_tag):
        print(f"Invalid release tag format: {release_tag}. Expected vX.Y.Z")
        sys.exit(1)

    # Determine the branch based on release type
    current_version = release_tag.lstrip('v').split('.')
    current_major, current_minor, current_patch = int(current_version[0]), int(current_version[1]), int(current_version[2])

    if current_patch == 0:
        # Minor/Major release: always on default branch
        current_branch = repo.default_branch
    else:
        # Patch release: on release-x.y branch
        current_branch = f"release-{current_major}.{current_minor}"

    # Verify the target branch actually exists
    try:
        repo.get_branch(current_branch)
    except UnknownObjectException:
        raise RuntimeError(f"Branch '{current_branch}' not found in the repository.")

    pulls = repo.get_pulls(state='closed', sort='created', base=current_branch, direction='desc')

    # get the last release tag
    tags = repo.get_tags()
    if tags.totalCount == 0:
        print("no tags in the repo")
        sys.exit()
    elif tags.totalCount == 1:
        last_release_tag = tags[0].name
    else:
        # Determine what the previous version should be
        if current_patch > 0:
            # Patch release: find x.y.(z-1)
            target_version = f"v{current_major}.{current_minor}.{current_patch - 1}"
        elif current_minor > 0:
            # Minor release: find x.(y-1).z (latest patch of previous minor)
            target_minor = current_minor - 1
            target_version_prefix = f"v{current_major}.{target_minor}."
            target_version = f"v{current_major}.{target_minor}.0"
        else:
            # Major release: find (x-1).y.z (latest minor.patch of previous major)
            target_major = current_major - 1
            target_version_prefix = f"v{target_major}."

            # Find all versions for the previous major release
            previous_versions = []
            for tag in tags:
                if tag.name.startswith(target_version_prefix):
                    try:
                        version_parts = tag.name.lstrip('v').split('.')
                        if len(version_parts) == 3:
                            minor_num = int(version_parts[1])
                            patch_num = int(version_parts[2])
                            previous_versions.append((minor_num, patch_num))
                    except ValueError:
                        continue

            if previous_versions:
                # Find the latest minor.patch combination
                latest_minor = max(v[0] for v in previous_versions)
                target_version = f"v{target_major}.{latest_minor}.0"
            else:
                target_version = f"v{target_major}.0.0"

        # Find the target version in tags
        last_release_tag = None
        for tag in tags:
            if tag.name == target_version:
                last_release_tag = tag.name
                break

        if not last_release_tag:
            print(f"Could not find previous release tag {target_version}")
            sys.exit()

    # get related PR from the last release tag
    last_release_pr = 0
    if last_release_tag:
        for tag in tags:
            if tag.name == last_release_tag:
                tag_pulls = tag.commit.get_pulls()
                if tag_pulls.totalCount > 0:
                    last_release_pr = tag_pulls[0].number
                break

    # collect all PRs from the last release tag
    features = []
    bugs = []
    breakings = []
    docs = []
    infras = []
    uncategorized = []
    for pr in pulls:
        if pr.number == last_release_pr:
            break
        if pr.is_merged():
            prtype, title = pr_type_from_title(pr.title)
            if prtype == PRType.FeaturePR:
                features.append(change_entry(title, pr.number, pr.user.login))
            elif prtype == PRType.BugfixPR:
                bugs.append(change_entry(title, pr.number, pr.user.login))
            elif prtype == PRType.DocsPR:
                docs.append(change_entry(title, pr.number, pr.user.login))
            elif prtype == PRType.InfraPR:
                infras.append(change_entry(title, pr.number, pr.user.login))
            elif prtype == PRType.BreakingPR:
                breakings.append(change_entry(title, pr.number, pr.user.login))
            elif prtype == PRType.UncategorizedPR:
                uncategorized.append(change_entry(title, pr.number, pr.user.login))

    # Print
    print("# Open Cluster Management %s" % release_tag)
    print("\n**changes since [%s](https://github.com/open-cluster-management-io/releases/%s)**\n"
          % (last_release_tag, last_release_tag))
    section_if_present(breakings, ":warning: Breaking Changes")
    section_if_present(features, ":sparkles: New Features")
    section_if_present(bugs, ":bug: Bug Fixes")
    section_if_present(docs, ":book: Documentation")
    section_if_present(infras, ":seedling: Infra & Such")
    print("")
    print("Thanks to all our contributors!*")
