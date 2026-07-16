// Availability signal for the AI Agent surface (#596 / unified AI drawer).
//
// The unified Silt AI drawer is the agent's surface: the titlebar control and
// the slash-command entry are gated on this flag. It flips true only when the
// silt-ai-agent plugin is enabled and has run onVaultOpen, which is also when a
// valid plugin session exists for the drawer's privileged SDK calls (loader.ts
// registers a session token only for enabled first-party plugins). This mirrors
// the aiAssistantChrome / writingAssistantChrome availability pattern.
//
// NOTE: the live chat controller is createAIChatController() in
// //frontend/src/plugins/shared/ai-chat/ai-chat-controller.svelte.ts, consumed
// by AIChatDrawer. This module intentionally owns NO controller, transcript, or
// send/cancel logic — only the availability flag the host shell gates on.

export const agentChrome = $state({ available: false })
