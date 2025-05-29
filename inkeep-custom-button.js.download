document.addEventListener("DOMContentLoaded", () => {
    const inkeepScript = document.createElement("script");
    inkeepScript.src = "https://unpkg.com/@inkeep/cxkit-js@0.5.67/dist/embed.js";
    inkeepScript.type = "module";
    inkeepScript.defer = true;
    document.head.appendChild(inkeepScript);

    const addInkeepWidget = ({ openChange }) => {
        return Inkeep.ChatButton({
            baseSettings: {
                env: "production",
                apiKey: "40582708b8a0305555fa91c049bb0dfa4e192337819bd03c", // required - replace with your own API key
                primaryBrandColor: "#fbc828", // your brand color, widget color scheme is derived from this
                organizationDisplayName: "BNBChain",
                colorMode: {
                    forcedColorMode: "dark",
                },
                theme: {
                    styles: [
                        {
                            key: "custome-theme",
                            type: "style",
                            value: `
                                :root {
                                    font-size: 16px;
                                }
                                .ikp-markdown-link {
                                    color: #fbc828;
                                    text-decoration: underline;
                                }
                                .ikp-chat-button__container .ikp-chat-button__button {
                                    font-size: 16px;
                                    padding: 8px 16px;
                                    height: 48px;
                                }
                                .ikp-chat-button__container .ikp-chat-button__avatar-image{
                                    width: 24px;
                                    height: 24px;
                                }
                            `,
                        },
                    ],
                },
            },
            aiChatSettings: {
                aiAssistantName: "BNB Chain AI",
                aiAssistantAvatar: `https://static.bnbchain.org/home-ui/static/images/logo.svg`,
                introMessage: `Yo dev!\nI'm your AI-powered sidekick for all things BNB Chain — smart contracts, SDKs, RPCs, you name it.`,
                disclaimerSettings: {
                    isEnabled: true,
                    label: 'Disclaimer',
                    tooltip:
                        'My answers might not always be 100% accurate. Double-check when in doubt. <a target="_blank" href="/en/ai-bot-disclaimer">Learn More<a>',
                },
                exampleQuestions: [
                    "How to deploy a BEP-20 token on BSC?",
                    "How to write a contract on opBNB?",
                    "How to use Greenfield for file storage?",
                    "Where can I find funding or grants?",
                ],
            },
            modalSettings: {
                onOpenChange: openChange
            }
        });
    };

    // Function to bind the click event to .ai-bot-wrapper
    const bindAiBotWrapperEvent = (widget) => {
        const aiBotWrapper = document.querySelector('.ai-bot-wrapper');
        if (aiBotWrapper) {
            aiBotWrapper.replaceWith(aiBotWrapper.cloneNode(true));
            const newAiBotWrapper = document.querySelector('.ai-bot-wrapper');
            newAiBotWrapper.addEventListener('click', () => {
                widget.update?.({ modalSettings: { isOpen: true } });
                window.dataLayer?.push({ event: "click_AiBot_floatBtn" });
            });
        } else {
            console.warn('.ai-bot-wrapper element not found. Ensure it is rendered correctly.');
        }
    };

    inkeepScript.addEventListener("load", () => {
        const widget = addInkeepWidget({
            openChange(isOpen) {
                widget.update?.({ modalSettings: { isOpen } });
            }
        });

        bindAiBotWrapperEvent(widget);

        const originalPushState = history.pushState;
        history.pushState = function (...args) {
            originalPushState.apply(this, args);
            setTimeout(() => bindAiBotWrapperEvent(widget), 300);
            setTimeout(() => bindAiBotWrapperEvent(widget), 1000);
        };

        window.addEventListener('popstate', () => {
            setTimeout(() => bindAiBotWrapperEvent(widget), 300);
            setTimeout(() => bindAiBotWrapperEvent(widget), 1000);
        });
    });
});