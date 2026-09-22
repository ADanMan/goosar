import localFont from 'next/font/local';

export const comfortaa = localFont({
  src: '../public/fonts/Comfortaa.ttf',
  weight: '300 700',
  display: 'swap',
  variable: '--font-comfortaa',
});

export const montserrat = localFont({
  src: [
    {
      path: '../public/fonts/Montserrat.ttf',
      weight: '100 900',
      style: 'normal',
    },
    {
      path: '../public/fonts/Montserrat-Italic.ttf',
      weight: '100 900',
      style: 'italic',
    },
  ],
  display: 'swap',
  variable: '--font-montserrat',
});
