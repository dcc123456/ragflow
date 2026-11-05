import { createRoot } from 'react-dom/client';

const showModal = ({ children }: { children: React.ReactNode }) => {
  let currentModal: any;
  const rootElement = document.createElement('div');
  document.body.appendChild(rootElement);

  const reactRoot = createRoot(rootElement);
  const closeModal = () => {
    if (reactRoot) {
      reactRoot.unmount();
    }
    if (currentModal) {
      currentModal = null;
    }
  };

  reactRoot.render(children);

  currentModal = { destroy: closeModal };

  return currentModal;
};

export { showModal };
