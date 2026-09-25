import ReactDOM from 'react-dom/client';
import { configStore } from '@goosar/core/config';
import App from './App';
import './brand-fonts.css';
import './globals.css';

if (import.meta.env.DEV && import.meta.env.VITE_REACT_GRAB) {
  const grab = document.createElement('script');
  grab.src = '//unpkg.com/react-grab/dist/index.global.js';
  grab.crossOrigin = 'anonymous';
  document.head.appendChild(grab);
}

configStore.getState().setMcpPresetOverlay(window.desktopAPI.presetOverlay);
configStore.getState().setDeploymentHosts(window.desktopAPI.deploymentHosts);

ReactDOM.createRoot(document.getElementById('root')!).render(<App />);
