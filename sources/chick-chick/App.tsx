import React, { useState, useEffect } from 'react';
import { Canvas } from '@react-three/fiber';
import { VoxelGame } from './components/VoxelGame';
import { GameStatus, DeathReason } from './types';
import { SKINS } from './constants';

function App() {
  const [status, setStatus] = useState<GameStatus>(GameStatus.MENU);
  const [score, setScore] = useState<number>(0);
  const [highScore, setHighScore] = useState<number>(0);
  const [coins, setCoins] = useState<number>(0);
  const [deathReason, setDeathReason] = useState<DeathReason>('FLATTENED');

  const [unlockedSkins, setUnlockedSkins] = useState<string[]>(['chicken', 'pig', 'penguin', 'bear']);
  const [currentSkin, setCurrentSkin] = useState<string>('chicken');

  // Mobile controls handler
  const handleMobileControl = (direction: 'up' | 'down' | 'left' | 'right') => {
    if (status !== GameStatus.PLAYING) return;
    const event = new KeyboardEvent('keydown', {
      key: direction === 'up' ? 'ArrowUp' :
           direction === 'down' ? 'ArrowDown' :
           direction === 'left' ? 'ArrowLeft' : 'ArrowRight'
    });
    window.dispatchEvent(event);
  };

  useEffect(() => {
    const storedBest = localStorage.getItem('voxel-best');
    if (storedBest) setHighScore(parseInt(storedBest, 10));
    
    const storedCoins = localStorage.getItem('voxel-coins');
    if (storedCoins) setCoins(parseInt(storedCoins, 10));

    const storedSkins = localStorage.getItem('voxel-skins');
    if (storedSkins) setUnlockedSkins(JSON.parse(storedSkins));

    const storedCurrent = localStorage.getItem('voxel-current-skin');
    if (storedCurrent) setCurrentSkin(storedCurrent);
  }, []);

  useEffect(() => {
    if (score > highScore) {
      setHighScore(score);
      localStorage.setItem('voxel-best', score.toString());
    }
  }, [score, highScore]);

  useEffect(() => {
    localStorage.setItem('voxel-coins', coins.toString());
  }, [coins]);

  useEffect(() => {
    localStorage.setItem('voxel-skins', JSON.stringify(unlockedSkins));
  }, [unlockedSkins]);

  useEffect(() => {
    localStorage.setItem('voxel-current-skin', currentSkin);
  }, [currentSkin]);

  const buySkin = (skin: typeof SKINS[0]) => {
      if (coins >= skin.price && !unlockedSkins.includes(skin.id)) {
          setCoins(c => c - skin.price);
          setUnlockedSkins(prev => [...prev, skin.id]);
          setCurrentSkin(skin.id);
      } else if (unlockedSkins.includes(skin.id)) {
          setCurrentSkin(skin.id);
      }
  };

  const getDeathText = (reason: DeathReason) => {
      switch(reason) {
          case 'FLATTENED': return 'SPLAT!';
          case 'DROWNED': return 'SPLASH!';
          case 'SMASHED': return 'TRAIN WRECK!';
          case 'LOST': return 'LOST IN VOID';
          default: return 'GAME OVER';
      }
  }

  return (
    <div className="relative w-full h-full bg-[#87CEEB] select-none overflow-hidden font-['Fredoka_Variable']">
      
      {/* 3D Game Layer */}
      <div className="absolute inset-0 z-0">
        <Canvas shadows dpr={[1, 1.5]}>
          <VoxelGame 
            status={status} 
            setStatus={setStatus} 
            setScore={setScore} 
            setCoins={setCoins} 
            currentSkin={currentSkin} 
            highScore={highScore}
            onDeath={setDeathReason}
          />
        </Canvas>
      </div>

      {/* UI Overlay */}
      <div className="absolute inset-0 z-10 pointer-events-none flex flex-col justify-between p-6 pb-2">
        
        {/* Top HUD */}
        <div className="flex justify-between items-start w-full pointer-events-auto">
            <div className="flex flex-col gap-3 transition-all duration-300">
                <div className="bg-white/90 backdrop-blur-md p-3 px-6 rounded-2xl shadow-lg border-b-4 border-slate-200 hover:scale-105 transition-transform">
                    <p className="text-xs font-bold text-slate-400 uppercase tracking-widest mb-1">Score</p>
                    <p className="text-4xl font-black text-slate-800 leading-none">{score}</p>
                </div>
                <div className="bg-yellow-400/90 backdrop-blur-md p-3 px-6 rounded-2xl shadow-lg border-b-4 border-yellow-600 flex items-center gap-3 hover:scale-105 transition-transform">
                    <div className="w-6 h-6 rounded-full bg-yellow-200 border-2 border-yellow-600 flex items-center justify-center text-[10px] shadow-inner">
                        $
                    </div>
                    <p className="text-2xl font-black text-yellow-900 leading-none">{coins}</p>
                </div>
            </div>
            <div className="bg-slate-800/90 backdrop-blur-md p-3 px-6 rounded-2xl shadow-lg border-b-4 border-slate-950 text-right hover:scale-105 transition-transform">
                <p className="text-xs font-bold text-slate-400 uppercase tracking-widest mb-1">Best</p>
                <p className="text-4xl font-black text-white leading-none">{highScore}</p>
            </div>
        </div>

        {/* Start Screen */}
        {status === GameStatus.MENU && (
           <div className="absolute inset-0 flex items-center justify-center bg-black/40 backdrop-blur-sm pointer-events-auto transition-all duration-500">
              <div className="bg-white p-10 rounded-3xl shadow-2xl text-center max-w-md border-b-8 border-slate-200 transform hover:scale-[1.02] transition-transform">
                 <h1 className="text-6xl font-black text-slate-800 mb-2 leading-none tracking-tight text-shadow-lg">
                    VOXEL<br/><span className="text-blue-500">HOPPER</span>
                 </h1>
                 <p className="text-slate-400 font-bold mb-8">Can you cross the road?</p>
                 
                 <button 
                   onClick={() => setStatus(GameStatus.PLAYING)}
                   className="group relative w-full mb-4 bg-blue-500 hover:bg-blue-400 text-white text-3xl font-black py-5 px-8 rounded-2xl shadow-[0_6px_0_#1e40af] active:shadow-none active:translate-y-[6px] transition-all"
                 >
                   HOP TO IT!
                 </button>
                 <button 
                   onClick={() => setStatus(GameStatus.SHOP)}
                   className="group w-full bg-purple-500 hover:bg-purple-400 text-white text-xl font-black py-4 px-8 rounded-2xl shadow-[0_6px_0_#6b21a8] active:shadow-none active:translate-y-[6px] transition-all"
                 >
                   SKIN SHOP
                 </button>
              </div>
           </div>
        )}

        {/* Shop Screen */}
        {status === GameStatus.SHOP && (
            <div className="absolute inset-0 flex items-center justify-center bg-purple-900/80 backdrop-blur-sm pointer-events-auto">
                <div className="bg-white w-full max-w-3xl p-8 rounded-[2.5rem] shadow-2xl border-b-8 border-slate-200 flex flex-col max-h-[85vh] transform transition-all">
                     <div className="flex justify-between items-center mb-8">
                        <h2 className="text-5xl font-black text-purple-600 text-shadow">WARDROBE</h2>
                        <div className="bg-yellow-100 px-6 py-3 rounded-2xl border-4 border-yellow-400 font-black text-yellow-800 flex items-center gap-2 text-xl shadow-sm">
                             <span>💰</span> {coins}
                        </div>
                     </div>
                     
                     <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 overflow-y-auto p-2 mb-4 custom-scrollbar">
                         {SKINS.map(skin => {
                             const owned = unlockedSkins.includes(skin.id);
                             const selected = currentSkin === skin.id;
                             return (
                                 <div 
                                    key={skin.id}
                                    onClick={() => buySkin(skin)}
                                    className={`relative p-4 rounded-2xl border-[3px] cursor-pointer transition-all hover:scale-105 active:scale-95
                                        ${selected ? 'border-green-500 bg-green-50 shadow-[0_4px_0_#22c55e]' : 
                                          owned ? 'border-slate-200 bg-slate-50 shadow-[0_4px_0_#e2e8f0]' : 
                                          'border-slate-300 bg-slate-100 grayscale opacity-80 shadow-[0_4px_0_#cbd5e1]'}
                                    `}
                                 >
                                     <div className="w-20 h-20 mx-auto mb-3 rounded-xl shadow-inner flex items-center justify-center" style={{ backgroundColor: skin.color }}>
                                        {/* Simple visualization of model */}
                                        <div className="w-10 h-10 bg-black/20 rounded-md"></div>
                                     </div>
                                     <p className="font-bold text-center text-slate-700 text-sm uppercase tracking-wide">{skin.name}</p>
                                     {!owned ? (
                                         <p className="text-center font-black text-yellow-600 mt-2 bg-yellow-100 rounded-lg py-1">{skin.price} 💰</p>
                                     ) : (
                                         selected && <div className="absolute -top-2 -right-2 bg-green-500 text-white rounded-full w-8 h-8 flex items-center justify-center font-bold shadow-md">✓</div>
                                     )}
                                 </div>
                             )
                         })}
                     </div>

                     <button 
                        onClick={() => setStatus(GameStatus.MENU)}
                        className="mt-auto bg-slate-200 hover:bg-slate-300 text-slate-600 font-black py-4 rounded-2xl shadow-[0_4px_0_#cbd5e1] active:shadow-none active:translate-y-[4px] transition-all uppercase tracking-wider"
                     >
                        Back to Menu
                     </button>
                </div>
            </div>
        )}

        {/* Game Over Screen */}
        {status === GameStatus.GAME_OVER && (
           <div className="absolute inset-0 flex items-center justify-center bg-red-900/60 backdrop-blur-md pointer-events-auto">
              <div className="bg-white p-10 rounded-[2.5rem] shadow-2xl text-center max-w-md border-b-[10px] border-red-900 animate-bounce-short transform hover:scale-105 transition-transform duration-300">
                 <h2 className="text-6xl font-black text-red-500 mb-2 transform -rotate-2 text-shadow-lg tracking-tighter uppercase">{getDeathText(deathReason)}</h2>
                 <p className="text-slate-400 text-xl font-bold mb-8 uppercase tracking-widest">Final Score: <span className="text-slate-800 text-3xl">{score}</span></p>
                 
                 <div className="flex flex-col gap-4">
                    <button 
                        onClick={() => { setScore(0); setStatus(GameStatus.PLAYING); }}
                        className="w-full bg-green-500 hover:bg-green-400 text-white text-2xl font-black py-4 rounded-2xl shadow-[0_6px_0_#166534] active:shadow-none active:translate-y-[6px] transition-all"
                    >
                        TRY AGAIN
                    </button>
                    <div className="flex gap-4">
                        <button 
                            onClick={() => setStatus(GameStatus.SHOP)}
                            className="flex-1 bg-purple-500 hover:bg-purple-400 text-white text-lg font-black py-4 rounded-2xl shadow-[0_6px_0_#6b21a8] active:shadow-none active:translate-y-[6px] transition-all"
                        >
                            SHOP
                        </button>
                        <button 
                            onClick={() => setStatus(GameStatus.MENU)}
                            className="flex-1 bg-slate-500 hover:bg-slate-400 text-white text-lg font-black py-4 rounded-2xl shadow-[0_6px_0_#334155] active:shadow-none active:translate-y-[6px] transition-all"
                        >
                            MENU
                        </button>
                    </div>
                 </div>
              </div>
           </div>
        )}

        {/* Mobile Controls - Only show during gameplay */}
        {status === GameStatus.PLAYING && (
          <div className="w-full flex justify-center items-end pb-6 pointer-events-auto">
            <div className="flex flex-col gap-4 items-center">
              {/* Top row: Up button */}
              <button
                onTouchStart={(e) => { e.preventDefault(); handleMobileControl('up'); }}
                onClick={() => handleMobileControl('up')}
                className="w-24 h-24 bg-blue-500/50 backdrop-blur-md rounded-2xl shadow-lg border-b-4 border-blue-700/50 active:shadow-none active:translate-y-1 active:border-b-0 transition-all flex items-center justify-center text-5xl font-black text-white hover:bg-blue-400/60"
              >
                ▲
              </button>

              {/* Bottom row: Left, Down, Right */}
              <div className="flex gap-4">
                <button
                  onTouchStart={(e) => { e.preventDefault(); handleMobileControl('left'); }}
                  onClick={() => handleMobileControl('left')}
                  className="w-24 h-24 bg-blue-500/50 backdrop-blur-md rounded-2xl shadow-lg border-b-4 border-blue-700/50 active:shadow-none active:translate-y-1 active:border-b-0 transition-all flex items-center justify-center text-5xl font-black text-white hover:bg-blue-400/60"
                >
                  ◀
                </button>

                <button
                  onTouchStart={(e) => { e.preventDefault(); handleMobileControl('down'); }}
                  onClick={() => handleMobileControl('down')}
                  className="w-24 h-24 bg-blue-500/50 backdrop-blur-md rounded-2xl shadow-lg border-b-4 border-blue-700/50 active:shadow-none active:translate-y-1 active:border-b-0 transition-all flex items-center justify-center text-5xl font-black text-white hover:bg-blue-400/60"
                >
                  ▼
                </button>

                <button
                  onTouchStart={(e) => { e.preventDefault(); handleMobileControl('right'); }}
                  onClick={() => handleMobileControl('right')}
                  className="w-24 h-24 bg-blue-500/50 backdrop-blur-md rounded-2xl shadow-lg border-b-4 border-blue-700/50 active:shadow-none active:translate-y-1 active:border-b-0 transition-all flex items-center justify-center text-5xl font-black text-white hover:bg-blue-400/60"
                >
                  ▶
                </button>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

export default App;