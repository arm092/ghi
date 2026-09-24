package am.ghi.ide;

import com.google.gson.*;
import com.intellij.execution.process.ProcessHandler;
import com.intellij.execution.process.ProcessAdapter;
import com.intellij.execution.process.ProcessEvent;
import com.intellij.execution.ui.ExecutionConsole;
import com.intellij.openapi.application.ApplicationManager;
import com.intellij.openapi.editor.Document;
import com.intellij.openapi.editor.EditorFactory;
import com.intellij.openapi.fileTypes.FileType;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.LocalFileSystem;
import com.intellij.ui.ColoredTextContainer;
import com.intellij.ui.SimpleTextAttributes;
import com.intellij.xdebugger.*;
import com.intellij.xdebugger.breakpoints.*;
import com.intellij.xdebugger.evaluation.XDebuggerEditorsProvider;
import com.intellij.xdebugger.evaluation.EvaluationMode;
import com.intellij.xdebugger.frame.*;
import org.jetbrains.annotations.NotNull;

import java.io.IOException;
import java.net.InetSocketAddress;
import java.nio.file.Path;
import java.util.*;
import java.util.concurrent.*;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.function.BiConsumer;

/** Ghi presentation on top of the Delve server bundled with GoLand. */
final class GhiDebugProcess extends XDebugProcess {
    private final ProcessHandler handler;
    private final ExecutionConsole console;
    private final GhiDebugNames names;
    private final Path root;
    private final ExecutorService worker=Executors.newSingleThreadExecutor(task->{Thread thread=new Thread(task,"Ghi debugger");thread.setDaemon(true);return thread;});
    private final Map<XLineBreakpoint<GhiBreakpointType.Properties>,Integer> breakpointIds=new ConcurrentHashMap<>();
    private final List<CompletableFuture<?>> initial=new CopyOnWriteArrayList<>();
    private final XBreakpointHandler<?>[] handlers={new Lines()};
    private volatile GhiDelve delve;
    private volatile boolean starting=true;
    private volatile CompletableFuture<?> activeCommand;
    private final AtomicBoolean stopped=new AtomicBoolean();
    private final AtomicBoolean editingBreakpoints=new AtomicBoolean();

    GhiDebugProcess(XDebugSession session,ProcessHandler handler,ExecutionConsole console,GhiDebugNames names,Path root){
        super(session);this.handler=handler;this.console=console;this.names=names;this.root=root;session.setPauseActionSupported(true);
        handler.addProcessListener(new ProcessAdapter(){@Override public void processTerminated(@NotNull ProcessEvent event){
            try{GhiDelve client=delve;if(client!=null)client.close();}catch(IOException ignored){}
            if(!session.isStopped())session.stop();
        }});
    }
    void connect(InetSocketAddress address){
        worker.execute(()->{
            try{
                IOException last=null;
                for(int attempt=0;attempt<300&&!handler.isProcessTerminated();attempt++){
                    try{delve=new GhiDelve(address);break;}
                    catch(IOException error){last=error;Thread.sleep(100);}
                }
                if(delve==null)throw last==null?new IOException("Delve exited before connecting"):last;
                ApplicationManager.getApplication().invokeAndWait(()->getSession().initBreakpoints());
                CompletableFuture.allOf(initial.toArray(CompletableFuture[]::new)).get(15,TimeUnit.SECONDS);
                starting=false;
                command("continue");
            }catch(Exception error){getSession().reportError("Ghi debugger: "+error.getMessage());getSession().stop();}
        });
    }
    @Override public boolean checkCanInitBreakpoints(){return false;}
    @Override public XBreakpointHandler<?> @NotNull [] getBreakpointHandlers(){return handlers;}
    @Override protected ProcessHandler doGetProcessHandler(){return handler;}
    @Override public ExecutionConsole createConsole(){return console;}
    @Override public XDebuggerEditorsProvider getEditorsProvider(){return new XDebuggerEditorsProvider(){
        @Override public @NotNull FileType getFileType(){return GhiFileType.INSTANCE;}
        @Override public @NotNull Document createDocument(@NotNull Project project,@NotNull XExpression expression,XSourcePosition position,@NotNull EvaluationMode mode){
            return EditorFactory.getInstance().createDocument(expression.getExpression());
        }
        @Override public boolean isEvaluateExpressionFieldEnabled(){return false;}
    };}
    @Override public void resume(XSuspendContext context){command("continue");}
    @Override public void startStepInto(XSuspendContext context){command("step");}
    @Override public void startStepOver(XSuspendContext context){command("next");}
    @Override public void startStepOut(XSuspendContext context){command("stepOut");}
    @Override public void startPausing(){worker.execute(()->command("halt"));}
    @Override public void stop(){
        if(!stopped.compareAndSet(false,true))return;
        GhiDelve client=delve;
        if(client!=null){
            JsonObject halt=new JsonObject();halt.addProperty("name","halt");
            client.request("Command",halt).orTimeout(2,TimeUnit.SECONDS).handle((result,error)->null).thenCompose(ignored->{
                JsonObject detach=new JsonObject();detach.addProperty("Kill",true);
                return client.request("Detach",detach).orTimeout(2,TimeUnit.SECONDS);
            }).whenComplete((result,error)->{
                try{client.close();}catch(IOException ignored){}
                terminateProcessTree();
            });
        }else terminateProcessTree();
        worker.shutdownNow();
    }
    private void terminateProcessTree(){
        if(handler instanceof com.intellij.execution.process.OSProcessHandler os){
            ProcessHandle process=os.getProcess().toHandle();
            process.descendants().forEach(ProcessHandle::destroyForcibly);
        }
        handler.destroyProcess();
    }
    private void command(String name){command(name,64);}
    private void command(String name,int remainingSteps){
        GhiDelve client=delve;if(client==null)return;
        JsonObject params=new JsonObject();params.addProperty("name",name);
        CompletableFuture<JsonObject> request=client.request("Command",params);
        activeCommand=request.whenComplete((result,error)->{
            if(error!=null){if(!handler.isProcessTerminated())getSession().reportError("Delve "+name+": "+error.getMessage());return;}
            JsonObject state=object(result,"State");
            if(bool(state,"exited")){stop();getSession().stop();return;}
            if(name.equals("continue")&&editingBreakpoints.get()&&string(state,"stopReason").equals("manual"))return;
            JsonObject thread=object(state,"currentThread");
            if(thread==null)return;
            JsonObject goroutine=object(state,"currentGoroutine");
            long id=number(goroutine,"id",number(thread,"goroutineID",-1));
            JsonObject trace=new JsonObject();trace.addProperty("Id",id);trace.addProperty("Depth",64);
            client.request("Stacktrace",trace).whenComplete((stack,stackError)->{
                if(stackError!=null){getSession().reportError("Delve stack: "+stackError.getMessage());return;}
                List<Frame> frames=new ArrayList<>();JsonArray locations=array(stack,"Locations");
                if((name.equals("step")||name.equals("stepOut"))&&remainingSteps>0&&locations!=null&&!locations.isEmpty()){
                    JsonObject first=locations.get(0).getAsJsonObject();
                    GhiDebugNames.Function function=names.function(string(object(first,"function"),"name"));
                    String source=string(first,"file");
                    if((function!=null&&function.helper())||!source.endsWith(".ghi")||source.endsWith(".ghi-runtime.ghi")){
                        command(name,remainingSteps-1);return;
                    }
                }
                if(locations!=null)for(int index=0;index<locations.size();index++){
                    JsonObject location=locations.get(index).getAsJsonObject();String function=string(object(location,"function"),"name");
                    GhiDebugNames.Function described=names.function(function);
                    if(described!=null&&described.helper())continue;
                    if(!string(location,"file").endsWith(".ghi"))continue;
                    if(string(location,"file").endsWith(".ghi-runtime.ghi"))continue;
                    frames.add(new Frame(id,index,location,described==null?function:described.name()));
                }
                if(frames.isEmpty()&&locations!=null&&!locations.isEmpty()){
                    JsonObject first=locations.get(0).getAsJsonObject();frames.add(new Frame(id,0,first,string(object(first,"function"),"name")));
                }
                if(frames.isEmpty())return;
                getSession().positionReached(new Suspended(new Stack(id,frames)));
            });
        });
    }
    private void changeBreakpoint(String operation,JsonObject params,BiConsumer<JsonObject,Throwable> completion){
        worker.execute(()->changeBreakpointNow(operation,params,completion));
    }
    private void changeBreakpointNow(String operation,JsonObject params,BiConsumer<JsonObject,Throwable> completion){
            GhiDelve client=delve;if(client==null||stopped.get())return;
            boolean resume=false;
            try{
                if(!getSession().isSuspended()){
                    editingBreakpoints.set(true);
                    CompletableFuture<?> interrupted=activeCommand;
                    JsonObject halt=new JsonObject();halt.addProperty("name","halt");
                    JsonObject state=object(client.request("Command",halt).get(3,TimeUnit.SECONDS),"State");
                    if(interrupted!=null)interrupted.get(3,TimeUnit.SECONDS);
                    resume=!bool(state,"exited")&&string(state,"stopReason").equals("manual");
                }
                completion.accept(client.request(operation,params).get(3,TimeUnit.SECONDS),null);
            }catch(Exception error){completion.accept(null,error);}
            finally{
                editingBreakpoints.set(false);
                if(resume&&!stopped.get()&&!getSession().isSuspended())command("continue");
            }
    }
    private final class Lines extends XBreakpointHandler<XLineBreakpoint<GhiBreakpointType.Properties>> {
        Lines(){super(GhiBreakpointType.class);}
        @Override public void registerBreakpoint(@NotNull XLineBreakpoint<GhiBreakpointType.Properties> breakpoint){
            GhiDelve client=delve;XSourcePosition position=breakpoint.getSourcePosition();
            if(client==null||position==null)return;
            if(!Path.of(position.getFile().getPath()).toAbsolutePath().normalize().startsWith(root))return;
            if(breakpoint.getConditionExpression()!=null||breakpoint.getLogExpressionObject()!=null){
                getSession().setBreakpointInvalid(breakpoint,"Ghi breakpoint expressions are not supported yet.");return;
            }
            JsonObject point=new JsonObject();point.addProperty("file",position.getFile().getPath());point.addProperty("line",position.getLine()+1);
            JsonObject params=new JsonObject();params.add("Breakpoint",point);
            BiConsumer<JsonObject,Throwable> complete=(result,error)->{
                if(error!=null)getSession().setBreakpointInvalid(breakpoint,error.getMessage());
                else{breakpointIds.put(breakpoint,(int)number(object(result,"Breakpoint"),"id",-1));getSession().setBreakpointVerified(breakpoint);}
            };
            if(starting){
                CompletableFuture<JsonObject> request=client.request("CreateBreakpoint",params);
                initial.add(request.handle((result,error)->null));request.whenComplete(complete);
            }else changeBreakpoint("CreateBreakpoint",params,complete);
        }
        @Override public void unregisterBreakpoint(@NotNull XLineBreakpoint<GhiBreakpointType.Properties> breakpoint,boolean temporary){
            if(starting){
                Integer id=breakpointIds.remove(breakpoint);GhiDelve client=delve;
                if(id!=null&&client!=null){JsonObject params=new JsonObject();params.addProperty("Id",id);client.request("ClearBreakpoint",params);}
                return;
            }
            worker.execute(()->{
                Integer id=breakpointIds.remove(breakpoint);
                if(id==null)return;
                JsonObject params=new JsonObject();params.addProperty("Id",id);
                changeBreakpointNow("ClearBreakpoint",params,(result,error)->{if(error!=null)getSession().reportError("Delve clear breakpoint: "+error.getMessage());});
            });
        }
    }
    private final class Suspended extends XSuspendContext {
        private final Stack stack;Suspended(Stack stack){this.stack=stack;}
        @Override public XExecutionStack getActiveExecutionStack(){return stack;}
        @Override public XExecutionStack[] getExecutionStacks(){return new XExecutionStack[]{stack};}
    }
    private final class Stack extends XExecutionStack {
        private final List<Frame> frames;
        Stack(long id,List<Frame> frames){super("Goroutine "+id);this.frames=frames;}
        @Override public XStackFrame getTopFrame(){return frames.getFirst();}
        @Override public void computeStackFrames(int first,XStackFrameContainer container){container.addStackFrames(frames.subList(Math.min(first,frames.size()),frames.size()),true);}
    }
    private final class Frame extends XStackFrame {
        private final long goroutine;private final int index;private final JsonObject location;private final String title;
        Frame(long goroutine,int index,JsonObject location,String title){this.goroutine=goroutine;this.index=index;this.location=location;this.title=title;}
        @Override public XSourcePosition getSourcePosition(){
            String path=string(location,"file");if(!path.endsWith(".ghi")||!Path.of(path).toAbsolutePath().normalize().startsWith(root))return null;
            var file=LocalFileSystem.getInstance().findFileByPath(path);return file==null?null:XDebuggerUtil.getInstance().createPosition(file,Math.max(0,(int)number(location,"line",1)-1));
        }
        @Override public void customizePresentation(@NotNull ColoredTextContainer component){component.append(title,SimpleTextAttributes.REGULAR_ATTRIBUTES);}
        @Override public void computeChildren(@NotNull XCompositeNode node){
            GhiDelve client=delve;if(client==null){node.setErrorMessage("Delve disconnected");return;}
            JsonObject scope=new JsonObject();scope.addProperty("GoroutineID",goroutine);scope.addProperty("Frame",index);
            JsonObject cfg=new JsonObject();cfg.addProperty("FollowPointers",true);cfg.addProperty("MaxVariableRecurse",4);
            cfg.addProperty("MaxStringLen",256);cfg.addProperty("MaxArrayValues",64);cfg.addProperty("MaxStructFields",-1);
            JsonObject params=new JsonObject();params.add("Scope",scope);params.add("Cfg",cfg);
            var locals=client.request("ListLocalVars",params);var args=client.request("ListFunctionArgs",params);
            locals.thenCombine(args,(left,right)->{
                var values=new XValueChildrenList();addValues(values,array(left,"Variables"));addValues(values,array(right,"Args"));
                return values;
            }).whenComplete((values,error)->{if(error!=null)node.setErrorMessage(error.getMessage());else node.addChildren(values,true);});
        }
        private void addValues(XValueChildrenList result,JsonArray values){
            if(values==null)return;
            for(JsonElement element:values){JsonObject value=element.getAsJsonObject();String name=string(value,"name");
                if(name.equals("ghi_flow")||name.isBlank())continue;
                result.add(name.equals("ghi_caught")?"caught exception":name,new Value(value));
            }
        }
    }
    private final class Value extends XValue {
        private final JsonObject value;Value(JsonObject value){this.value=value;}
        private JsonObject contents(){
            JsonObject current=value;
            String type=string(current,"type");
            if(names.hasType(type.replaceFirst("^\\*+",""))){
                for(int i=0;i<3;i++){
                    long kind=number(current,"kind",-1);
                    if(kind!=20&&kind!=22)break;
                    JsonArray children=array(current,"children");if(children==null||children.size()!=1)break;
                    JsonObject child=children.get(0).getAsJsonObject();String field=string(child,"name");
                    if(kind==20&&!field.equals("data"))break;
                    current=child;
                }
            }
            return current;
        }
        @Override public void computePresentation(@NotNull XValueNode node,@NotNull XValuePlace place){
            String type=string(value,"type");String plain=type.replaceFirst("^\\*+","");
            String title=type.substring(0,type.length()-plain.length())+names.type(plain);
            String text=string(value,"value");JsonObject actual=contents();
            JsonArray children=array(actual,"children");boolean expandable=children!=null&&!children.isEmpty();
            if(!string(value,"unreadable").isBlank())text=string(value,"unreadable");
            node.setPresentation(null,title,text,expandable);
        }
        @Override public void computeChildren(@NotNull XCompositeNode node){
            JsonArray children=array(contents(),"children");var result=new XValueChildrenList();
            if(children!=null)for(JsonElement element:children){JsonObject child=element.getAsJsonObject();
                String name=names.field(string(child,"name"));if(name.isBlank())name="value";
                result.add(name,new Value(child));
            }
            node.addChildren(result,true);
        }
    }
    private static JsonObject object(JsonObject value,String key){JsonElement child=value==null?null:value.get(key);return child!=null&&child.isJsonObject()?child.getAsJsonObject():null;}
    private static JsonArray array(JsonObject value,String key){JsonElement child=value==null?null:value.get(key);return child!=null&&child.isJsonArray()?child.getAsJsonArray():null;}
    private static String string(JsonObject value,String key){JsonElement child=value==null?null:value.get(key);return child==null||child.isJsonNull()?"":child.getAsString();}
    private static long number(JsonObject value,String key,long fallback){JsonElement child=value==null?null:value.get(key);return child==null||child.isJsonNull()?fallback:child.getAsLong();}
    private static boolean bool(JsonObject value,String key){JsonElement child=value==null?null:value.get(key);return child!=null&&!child.isJsonNull()&&child.getAsBoolean();}
}
