import clsx from 'clsx'

import { useI18n } from '../../i18n'
import { useApiTesterState } from './useApiTesterState'
import { useChatStreamClient } from './useChatStreamClient'
import ConfigPanel from './ConfigPanel'
import ChatPanel from './ChatPanel'

export default function ApiTesterContainer({ config, onMessage, authFetch }) {
    const { t } = useI18n()

    const {
        model,
        setModel,
        message,
        setMessage,
        apiKey,
        setApiKey,
        selectedAccount,
        setSelectedAccount,
        response,
        setResponse,
        loading,
        setLoading,
        streamingContent,
        setStreamingContent,
        streamingThinking,
        setStreamingThinking,
        isStreaming,
        setIsStreaming,
        streamingMode,
        setStreamingMode,
        configExpanded,
        setConfigExpanded,
        abortControllerRef,
    } = useApiTesterState({ t })

    const accounts = config.accounts || []
    const resolveAccountIdentifier = (acc) => {
        if (!acc || typeof acc !== 'object') return ''
        return String(acc.identifier || acc.email || acc.mobile || '').trim()
    }
    const configuredKeys = config.keys || []
    const trimmedApiKey = apiKey.trim()
    const defaultKey = configuredKeys[0] || ''
    const effectiveKey = trimmedApiKey || defaultKey
    const customKeyActive = trimmedApiKey !== ''
    const customKeyManaged = customKeyActive && configuredKeys.includes(trimmedApiKey)

    const models = [
        { id: 'deepseek-chat', name: 'deepseek-chat', icon: 'MessageSquare', desc: t('apiTester.models.chat'), color: 'text-amber-500' },
        { id: 'deepseek-reasoner', name: 'deepseek-reasoner', icon: 'Cpu', desc: t('apiTester.models.reasoner'), color: 'text-amber-600' },
        { id: 'deepseek-v4-flash', name: 'deepseek-v4-flash', icon: 'Zap', desc: t('apiTester.models.v4Flash'), color: 'text-lime-600' },
        { id: 'deepseek-v4-flash-search', name: 'deepseek-v4-flash-search', icon: 'SearchIcon', desc: t('apiTester.models.v4FlashSearch'), color: 'text-lime-500' },
        { id: 'deepseek-v4-flash-think', name: 'deepseek-v4-flash-think', icon: 'Cpu', desc: t('apiTester.models.v4FlashThink'), color: 'text-lime-700' },
        { id: 'deepseek-v4-flash-think-search', name: 'deepseek-v4-flash-think-search', icon: 'SearchIcon', desc: t('apiTester.models.v4FlashThinkSearch'), color: 'text-teal-600' },
        { id: 'deepseek-v4-pro', name: 'deepseek-v4-pro', icon: 'Cpu', desc: t('apiTester.models.v4Pro'), color: 'text-indigo-600' },
        { id: 'deepseek-v4-pro-search', name: 'deepseek-v4-pro-search', icon: 'SearchIcon', desc: t('apiTester.models.v4ProSearch'), color: 'text-indigo-500' },
        { id: 'deepseek-v4-pro-think', name: 'deepseek-v4-pro-think', icon: 'Cpu', desc: t('apiTester.models.v4ProThink'), color: 'text-indigo-700' },
        { id: 'deepseek-v4-pro-think-search', name: 'deepseek-v4-pro-think-search', icon: 'SearchIcon', desc: t('apiTester.models.v4ProThinkSearch'), color: 'text-sky-600' },
        { id: 'deepseek-chat-search', name: 'deepseek-chat-search', icon: 'SearchIcon', desc: t('apiTester.models.chatSearch'), color: 'text-cyan-500' },
        { id: 'deepseek-reasoner-search', name: 'deepseek-reasoner-search', icon: 'SearchIcon', desc: t('apiTester.models.reasonerSearch'), color: 'text-cyan-600' },
    ]

    const { runTest, stopGeneration } = useChatStreamClient({
        t,
        onMessage,
        model,
        message,
        effectiveKey,
        selectedAccount,
        streamingMode,
        abortControllerRef,
        setLoading,
        setIsStreaming,
        setResponse,
        setStreamingContent,
        setStreamingThinking,
    })

    return (
        <div className={clsx('flex flex-col lg:grid lg:grid-cols-12 gap-6 h-[calc(100vh-140px)]')}>
            <ConfigPanel
                t={t}
                configExpanded={configExpanded}
                setConfigExpanded={setConfigExpanded}
                models={models}
                model={model}
                setModel={setModel}
                streamingMode={streamingMode}
                setStreamingMode={setStreamingMode}
                selectedAccount={selectedAccount}
                setSelectedAccount={setSelectedAccount}
                accounts={accounts}
                resolveAccountIdentifier={resolveAccountIdentifier}
                apiKey={apiKey}
                setApiKey={setApiKey}
                config={config}
                customKeyActive={customKeyActive}
                customKeyManaged={customKeyManaged}
            />

            <ChatPanel
                t={t}
                message={message}
                setMessage={setMessage}
                response={response}
                isStreaming={isStreaming}
                loading={loading}
                streamingThinking={streamingThinking}
                streamingContent={streamingContent}
                onRunTest={runTest}
                onStopGeneration={stopGeneration}
            />
        </div>
    )
}
