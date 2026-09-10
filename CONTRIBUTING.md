# Development and releases

`staging` is for cloud testing. `main` is for production. Configure the hosting
service to deploy each environment from its corresponding branch; merging a PR
updates GitHub, while the hosting service reports deployment success separately.

## Everyday workflow in T3 Code

1. Start each change on a **new feature branch from the latest `origin/staging`**.
   Use a separate worktree if another task is in progress. Do not reuse a branch
   after its PR has been squash-merged.
2. Make the change and run `go test ./...` and `go vet ./...`. Format changed Go
   files with `gofmt`. If SQL changes, follow the pinned sqlc instructions in
   README.md and regenerate the database code.
3. Commit, push the feature branch, and open a PR with **base `staging`**.
   Check the base explicitly: GitHub's default branch is `main`.
4. Wait for **Go checks** to pass, review the diff, and choose **Squash and merge**.
   Delete the feature branch when finished. Wait for the staging deployment to
   succeed and test the change in the cloud. Make fixes on a fresh feature branch.
5. When staging is ready for production, open a PR with **base `main`, head
   `staging`**. Use a descriptive title such as “Promote calendar scheduling to
   production” and summarize only the changes since the last promotion. Include
   what you tested in staging.
6. Wait for **Go checks** and choose **Create a merge commit**. Never squash or
   rebase a staging-to-main promotion: that causes old changes to reappear in
   later PRs. Keep both `main` and `staging` branches.
7. Wait for production deployment success. Bring the promotion commit back to
   staging with the synchronization commands below. This also triggers the
   staging deployment. Clean up finished feature worktrees only after checking
   for uncommitted files and local databases.

## Synchronize after promotion

Run these in the main checkout with a clean working tree and no concurrent
changes being pushed to staging:

```sh
git fetch --prune origin
git switch main
git merge --ff-only origin/main
git switch staging
git merge --ff-only origin/staging
git merge --ff-only origin/main
git push origin staging
git rev-list --left-right --count origin/main...origin/staging
```

The final output should be `0 0`. These commands do not overwrite local changes.
If a fast-forward fails, stop and inspect the commits; do not reset or force-push.
If new work reached staging during the promotion, main can be merged back into
staging through a regular merge PR instead. The branches will then intentionally
differ until the next promotion.

Local `main` and `staging` track their corresponding remote branches. Main
requires a PR and passing Go checks. Staging requires passing Go checks, while
allowing the tested main commit to be fast-forwarded back into it. Both branches
block force pushes and deletion. Administrator enforcement is enabled.

## Prompts to use in T3 Code

**Start a change:**

> Start a new feature branch from the latest origin/staging for this change:
> [describe change]. Preserve existing work. Implement and test it, then push
> and open a PR targeting staging. Do not merge or promote to main yet.

**Send it to staging:**

> Review this feature PR and its checks. If everything passes, squash-merge it
> into staging and delete the finished feature branch. Check the staging
> deployment status and give me the changes to test. Do not promote to main.

**Promote to production:**

> I have tested staging and want to promote it to production. Open a staging
> to main PR describing the new changes and testing. After checks pass, merge
> it using Create a merge commit, never squash or rebase. Verify the production
> deployment status, fast-forward staging to main if possible, and synchronize
> the local main and staging branches. Preserve any unfinished work.

## Version tags

For a milestone release, tag the verified production commit with an annotated
version such as `v0.1.0` and publish a GitHub release with concise release notes.
Use `v0.x.y` while pre-production. Do not move published tags or tag every staging
deployment. The deployed commit SHA identifies builds between releases.
