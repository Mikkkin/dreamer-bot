import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { createTelegram } from './telegram/bridge'
import './styles/tokens.css'
import './styles/base.css'
import './styles/components.css'
import './styles/screens.css'

const tg = createTelegram()
tg?.boot()

const container = document.getElementById('root')
if (!container) throw new Error('#root is missing from index.html')
createRoot(container).render(
  <StrictMode>
    <App tg={tg} />
  </StrictMode>,
)
