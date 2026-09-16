### Changed

- **The triage prompt puts project-stable content first, so a sweep prefills it
  once.** A triage turn is ~94% prefill — 5,600-7,500 tokens in, 24-44 out;
  measured on an 8B, 28.7 s of prefill against 1.9 s of decode — so the only
  optimisation that matters is not prefilling the same tokens twice, and every
  serving runtime in use (llama.cpp, ollama, vLLM, OVMS) caches a prompt PREFIX.

  The previous order defeated that completely: `Issue: #<n>` sat on the third
  line, so two prompts from the same sweep diverged after about forty tokens,
  while the genuinely stable sections — the open-issue list, the verification
  commands, the closing policy — sat at the END where a cache can never reach
  them. They now come first and the issue comes last.

  It is also the better order for the answer, which is why it is not a trade:
  the decisive evidence lands nearest the question, where attention is
  strongest.

  The open-issue list now includes the issue being triaged, because excluding
  it made the list differ per issue. The prompt says so explicitly — a list
  that silently contained the subject would invite the one duplicate verdict
  that is always wrong.
